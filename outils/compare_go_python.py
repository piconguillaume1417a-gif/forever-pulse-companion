#!/usr/bin/env python3
"""Test différentiel : le décodage Go (wowsync) doit donner EXACTEMENT les mêmes
personnages, deltas compris, et le même cumul que la référence Python.

    python outils/compare_go_python.py <wowsync> <dossier outils de référence> <fichier.lua> [...]

- <wowsync> : l'exécutable (wowsync, wowsync.exe) ou « go run ./cmd/wowsync ».
- <dossier outils> : forever-pulse-roadmap/outils (schema.py, cumul.py, simule.py).
- un fichier FPProbeDB (fixture du 18/09) est d'abord ré-emballé au schéma 4 par
  simule.py --avec-delta : c'est ce qui exerce une vraie chaîne de deltas.

Ce qui est comparé, lot par lot (clé batch_id) :
  * la liste ordonnée des personnages envoyés (GUID, nom, nom affiché, classe, race,
    sexe, niveau, royaume ; pour le /who : nom, classe et race localisées, niveau,
    guilde, zone) ;
  * puis le cumul complet, fichier après fichier (lots traités, dates, sources,
    valeurs retenues, table des noms), après sérialisation JSON des deux côtés.

Sortie : « IDENTIQUE » et code 0, ou la première divergence et code 1.
Aucun nom de personnage n'est imprimé : seulement des identifiants de lot et des comptes.
"""
import json
import os
import shlex
import subprocess
import sys
import tempfile


def main(argv):
    if len(argv) < 4:
        raise SystemExit(__doc__)
    go = shlex.split(argv[1])
    outils = os.path.abspath(argv[2])
    sys.path.insert(0, outils)
    from lua_tables import charger, liste  # noqa: E402
    from schema import decode_lot, lignes, lots_developpes, reconstruit  # noqa: E402
    import cumul  # noqa: E402

    fichiers = []
    tmp = tempfile.mkdtemp(prefix="compare-")
    for f in argv[3:]:
        d = charger(f)
        if "FPProbeDB" in d and "ForeverPulseCensusDB" not in d:
            sortie = os.path.join(tmp, os.path.basename(f) + ".schema4.lua")
            subprocess.run([sys.executable, os.path.join(outils, "simule.py"), f, sortie, "--avec-delta"],
                           check=True, stdout=subprocess.DEVNULL)
            f = sortie
        fichiers.append(f)

    # --- côté Python
    attendu = {}
    for f in fichiers:
        db = charger(f)["ForeverPulseCensusDB"]
        lots = lots_developpes(db)  # schéma 5 (addon 3.7.0) développé au schéma 4
        etats = reconstruit([l for l in lots if l.get("method") == "channel_roster"])
        for l in lots:
            bid = l.get("batch_id")
            if l.get("method") == "who_manual":
                champs = (l.get("fields") or "").split("|")
                persos = []
                for ligne in lignes(l.get("rows")):
                    c = dict(zip(champs, ligne.split("|")))
                    niv = c.get("level", "")
                    persos.append({"name": c.get("name", ""), "class_loc": c.get("class_loc", ""),
                                   "race_loc": c.get("race_loc", ""),
                                   "level": int(niv) if niv.isdigit() else None,
                                   "guild": c.get("guild", ""), "zone": c.get("zone", "")})
            else:
                persos = []
                for p in etats[bid].values():
                    persos.append({"guid": p["guid"], "name": p["name"], "display_name": p["display_name"],
                                   "class": p["class"], "race": p["race"],
                                   "sex": int(p["sex"]) if p["sex"].isdigit() else None,
                                   "level": p["level"], "realm": p["realm"]})
            attendu[bid] = persos
    c = cumul.nouveau()
    for f in fichiers:
        cumul.integrer_fichier(c, f)
    cumul_py = json.loads(json.dumps(c, sort_keys=True))

    # --- côté Go
    out = subprocess.run(go + ["--dump"] + fichiers, check=True, capture_output=True).stdout
    dump = json.loads(out)
    obtenu = {}
    invalides = []
    for fi in dump["fichiers"]:
        if fi["fatales"]:
            print(f"DIVERGENCE : fichier refusé par Go : {fi['path']} ({len(fi['fatales'])} défauts)")
            return 1
        for l in fi["lots"]:
            if not l["valide"]:
                invalides.append(l["batch_id"])
                continue
            obtenu[l["batch_id"]] = l["payload"]["characters"]
    if invalides:
        print(f"DIVERGENCE : {len(invalides)} lots écartés par Go, acceptés par la référence : {invalides[:3]}")
        return 1
    if set(attendu) != set(obtenu):
        print(f"DIVERGENCE : lots Python {len(attendu)}, lots Go {len(obtenu)}")
        return 1
    n_perso = 0
    for bid, persos in attendu.items():
        if persos != obtenu[bid]:
            for i, (a, b) in enumerate(zip(persos, obtenu[bid])):
                if a != b:
                    diff = sorted(k for k in set(a) | set(b) if a.get(k) != b.get(k))
                    print(f"DIVERGENCE : lot {bid}, personnage {i + 1}, champs {diff}")
                    return 1
            print(f"DIVERGENCE : lot {bid}, {len(persos)} personnages Python, {len(obtenu[bid])} Go")
            return 1
        n_perso += len(persos)
    cumul_go = json.loads(json.dumps(dump["cumul"], sort_keys=True))
    if cumul_py != cumul_go:
        for sid in sorted(set(cumul_py["scopes"]) | set(cumul_go["scopes"])):
            a, b = cumul_py["scopes"].get(sid), cumul_go["scopes"].get(sid)
            if a is None or b is None:
                print(f"DIVERGENCE : périmètre {sid} présent d'un seul côté")
                return 1
            for part in ("depuis", "noms", "persos"):
                if a[part] != b[part]:
                    if part == "persos":
                        cles = sorted(k for k in set(a[part]) | set(b[part]) if a[part].get(k) != b[part].get(k))
                        print(f"DIVERGENCE : cumul {sid}, {len(cles)} entrées différentes")
                    else:
                        print(f"DIVERGENCE : cumul {sid}, {part}")
                    return 1
        print("DIVERGENCE : cumul (lots traités ou version)")
        return 1
    total = sum(len(s["persos"]) for s in cumul_py["scopes"].values())
    print(f"IDENTIQUE : {len(fichiers)} fichiers, {len(attendu)} lots, {n_perso} lignes de personnages, "
          f"cumul de {total} personnages distincts ({len(cumul_py['lots_traites'])} lots traités)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
