#!/usr/bin/env python3
"""État de la file locale du compagnon, en comptes seulement, sans réseau.

    python outils/etat-file-locale.py [dossier]

- <dossier> : dossier des données du compagnon (défaut :
  %APPDATA%\\ForeverPulse\\Companion). Il n'est jamais modifié.

La base est en mode WAL. Le script refuse de travailler si le compagnon tourne,
puis copie ensemble companion.db, companion.db-wal et companion.db-shm (s'ils
existent) dans un dossier temporaire privé, vérifie que les trois fichiers n'ont
pas bougé pendant la copie, lit la COPIE (jamais l'original : même une ouverture
en lecture seule crée des fichiers -wal/-shm à côté de la base) puis efface la
copie.

Sortie : des nombres, des dates et des codes de refus du site. Jamais un nom,
un GUID, un identifiant de lot, une empreinte de fichier, un identifiant
d'observateur, un jeton ni un chemin.

Les critères « éligibles à l'essai STAGING » reprennent ceux de
forever-pulse/scripts/census/staging-beta-extract.py : lot original de zone
(channel_roster sans source_scope, jamais un canal personnalisé ni un /who),
fichier courant de l'addon 3.7.9, 1 à 50 personnages, observé dans les 29 jours,
périmètre de région 90.
"""
import json
import os
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time

FICHIERS = ("companion.db", "companion.db-wal", "companion.db-shm")


def compagnon_lance():
    if os.name != "nt":
        return False
    sortie = subprocess.run(["tasklist", "/FI", "IMAGENAME eq ForeverPulseCompanion.exe", "/NH"],
                            capture_output=True, text=True, check=False).stdout
    return "foreverpulsecompanion.exe" in sortie.lower()


def empreinte(dossier):
    etat = {}
    for nom in FICHIERS:
        p = os.path.join(dossier, nom)
        if os.path.isfile(p):
            s = os.stat(p)
            etat[nom] = (s.st_size, s.st_mtime_ns)
    return etat


def copie_coherente(dossier):
    avant = empreinte(dossier)
    if "companion.db" not in avant:
        raise SystemExit("companion.db absent")
    tmp = tempfile.mkdtemp(prefix="fpc-etat-")
    os.chmod(tmp, 0o700)
    for nom in avant:
        shutil.copyfile(os.path.join(dossier, nom), os.path.join(tmp, nom))
    if empreinte(dossier) != avant:
        shutil.rmtree(tmp, ignore_errors=True)
        raise SystemExit("la base a changé pendant la copie : compagnon lancé ? rien n'a été lu")
    return tmp, sorted(avant)


def un(c, sql, *args):
    return c.execute(sql, args).fetchone()


def date(t):
    return time.strftime("%Y-%m-%d %H:%M UTC", time.gmtime(t)) if t else "—"


def main(argv):
    dossier = argv[1] if len(argv) > 1 else os.path.join(os.environ.get("APPDATA", ""), "ForeverPulse", "Companion")
    if compagnon_lance():
        raise SystemExit("ForeverPulseCompanion.exe est lancé : fermez-le (Quitter), rien n'a été lu")
    tmp, copies = copie_coherente(dossier)
    try:
        c = sqlite3.connect(os.path.join(tmp, "companion.db"))
        c.execute("pragma query_only=on")
        print("copie cohérente de :", ", ".join(copies))
        print("intégrité (quick_check) :", un(c, "pragma quick_check")[0])

        print("\nfile d'envoi, par état : lots / personnages")
        for etat, n, p in c.execute("select state,count(*),coalesce(sum(characters),0) from queue group by state order by state"):
            print(f"  {etat:9} {n:6} / {p}")

        print("\nlots en attente, par source")
        classe = """case
            when json_extract(payload,'$.method')='who_manual' then '/who'
            when json_type(payload,'$.source_scope')='null' then 'zone'
            else coalesce(json_extract(payload,'$.source_scope'),'?') end"""
        for src, n, p in c.execute(f"select {classe} s,count(*),coalesce(sum(characters),0) from queue "
                                   "where state='pending' group by s order by s"):
            print(f"  {src:14} {n:6} lots, {p} personnages")

        print("\nlots en attente, par fichier d'origine et version d'addon")
        for origine, version, n in c.execute(
                "select f.origin,f.addon_version,count(*) from queue q join files f on f.sha256=q.file_sha256 "
                "where q.state='pending' group by 1,2 order by 1,2"):
            print(f"  {origine:8} addon {version:8} {n:6}")

        mini, maxi = un(c, "select min(observed_at),max(observed_at) from queue where state='pending'")
        print("\nobservés (en attente) : du", date(mini), "au", date(maxi))
        print("lots en attente déjà tentés :", un(c, "select count(*) from queue where state='pending' and attempts>0")[0])
        for raison, n in c.execute("select coalesce(reason,'?'),count(*) from queue where state='rejected' group by 1 order by 1"):
            print(f"refusés ({raison}) : {n}")

        maintenant = int(time.time())
        eligibles = 0
        for texte, scopes in c.execute(
                """select q.payload,f.scopes from queue q join files f on f.sha256=q.file_sha256
                   where q.state='pending' and f.origin='current' and f.addon_version='3.7.9'
                     and q.characters between 1 and 50 and q.observed_at between ? and ?
                     and json_extract(q.payload,'$.method')='channel_roster'
                     and json_type(q.payload,'$.source_scope')='null'""",
                (maintenant - 29 * 86400, maintenant + 300)):
            lot = json.loads(texte)
            portee = json.loads(scopes).get(lot.get("scope_id"))
            if isinstance(portee, dict) and portee.get("region") == "90" and 1 <= len(lot.get("characters") or []) <= 50:
                eligibles += 1
        print("\nlots éligibles à l'essai STAGING (zone, 3.7.9, 1-50, 29 jours, région 90) :", eligibles)

        print("\nfichiers lus :", un(c, "select count(*) from files")[0])
        for origine, n in c.execute("select origin,count(*) from files group by 1 order by 1"):
            print(f"  {origine:8} {n}")
        print("cumul : personnages", un(c, "select count(*) from cumul_persos")[0],
              "; périmètres", un(c, "select count(*) from cumul_scopes")[0],
              "(provenance insuffisante pour une publication : utiliser les lots de la file)")
        bloque = un(c, "select value from meta where key='blocked'")
        print("blocage enregistré :", (bloque[0] if bloque and bloque[0] else "aucun"))
        dernier = un(c, "select value from meta where key='last_send'")
        if dernier and dernier[0]:
            p = dernier[0].split("|")
            print(f"dernier envoi noté : {p[0]} — {p[1] if len(p) > 1 else '?'} lots, HTTP {p[3] if len(p) > 3 else '?'}")
        else:
            print("dernier envoi noté : aucun")
        c.close()
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    print("\ncopie effacée ; rien n'a été envoyé ni modifié")


if __name__ == "__main__":
    try:
        main(sys.argv)
    except sqlite3.Error as e:
        raise SystemExit(f"lecture impossible : {type(e).__name__}")
