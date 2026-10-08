"""Rend en PNG les scènes JSON de outils/apercu (aperçu hors Windows).

Approximation : Liberation Sans remplace Segoe UI (métrique proche d'Arial, un peu
plus large que Segoe UI, donc prudente pour les débordements) ; les glyphes Segoe
MDL2 Assets sont remplacés par des symboles DejaVu.

    python rendu.py apercu.json dossier [logo.png] [échelle]
"""
import json, os, sys
from PIL import Image, ImageDraw, ImageFont

SS = 3  # suréchantillonnage (anticrénelage)
REG = "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf"
BOLD = "/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf"
SYM = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
if os.name == "nt":
    fonts = os.path.join(os.environ.get("WINDIR", "C:/Windows"), "Fonts")
    REG, BOLD, SYM = [os.path.join(fonts, name) for name in ("segoeui.ttf", "segoeuib.ttf", "seguisym.ttf")]
GLYPHES = {"": "➤", "": "⎘", "": "↗", "": "☰", "": "◷",
           "": "◍", "": "✓", "": "↻", "": "⚠"}
_polices = {}


def police(chemin, taille):
    k = (chemin, taille)
    if k not in _polices:
        _polices[k] = ImageFont.truetype(chemin, taille)
    return _polices[k]


def rgb(c):
    return ((c >> 16) & 255, (c >> 8) & 255, c & 255)


def rendu(scene, logo, echelle):
    k = echelle * SS
    img = Image.new("RGB", (int(scene["Largeur"] * k), int(scene["Hauteur"] * k)))
    d = ImageDraw.Draw(img)

    def box(r, inset=0):
        return [r["X"] * k + inset, r["Y"] * k + inset, (r["X"] + r["W"]) * k - 1 - inset, (r["Y"] + r["H"]) * k - 1 - inset]

    def texte(op, contenu, chemin, souligne=False):
        f = police(chemin, max(1, round(op["Taille"] * k)))
        r = op["R"]
        x0, y0, w, h = r["X"] * k, r["Y"] * k, r["W"] * k, r["H"] * k
        asc, desc = f.getmetrics()
        lignes = [contenu]
        if op.get("Lignes"):
            lignes, cur = [], ""
            for mot in contenu.split(" "):
                essai = (cur + " " + mot).strip()
                if f.getlength(essai) <= w or not cur:
                    cur = essai
                else:
                    lignes.append(cur)
                    cur = mot
            lignes.append(cur)
            hl = asc + desc
            lignes = lignes[: max(1, int(h // hl))]
            y = y0
        else:
            s = contenu
            if f.getlength(s) > w:
                while s and f.getlength(s + "…") > w:
                    s = s[:-1]
                s += "…"
            lignes = [s]
            y = y0 + (h - (asc + desc)) / 2
        for l in lignes:
            lw = f.getlength(l)
            x = x0 if op["Aligne"] == 0 else (x0 + (w - lw) / 2 if op["Aligne"] == 1 else x0 + w - lw)
            d.text((x, y), l, font=f, fill=rgb(op["Couleur"]))
            if souligne:
                d.line([x, y + asc + k, x + lw, y + asc + k], fill=rgb(op["Couleur"]), width=max(1, int(k)))
            y += asc + desc

    for op in scene["Ops"]:
        g = op["Genre"]
        if g == 0:  # rectangle arrondi
            rayon = op["Rayon"] * k
            if not op.get("Vide"):
                d.rounded_rectangle(box(op["R"]), radius=rayon, fill=rgb(op["Couleur"]))
            if op.get("Epais", 0) > 0:
                d.rounded_rectangle(box(op["R"]), radius=rayon, outline=rgb(op["Bord"]), width=int(op["Epais"] * k))
        elif g == 1:  # dégradé vertical
            r = op["R"]
            a, b = rgb(op["Couleur"]), rgb(op["Couleur2"])
            for i in range(int(r["H"] * k)):
                t = i / max(1, r["H"] * k - 1)
                c = tuple(int(a[j] + (b[j] - a[j]) * t) for j in range(3))
                d.line([r["X"] * k, r["Y"] * k + i, (r["X"] + r["W"]) * k, r["Y"] * k + i], fill=c)
        elif g == 2:
            d.ellipse(box(op["R"]), fill=rgb(op["Couleur"]))
        elif g == 3:
            texte(op, op["Texte"], BOLD if op.get("Gras") else REG, op.get("Souligne"))
        elif g == 4:
            texte(dict(op, Taille=op["Taille"] * 1.1), GLYPHES.get(op["Texte"], "?"), SYM)
        elif g == 5 and logo:
            r = op["R"]
            l = logo.resize((int(r["W"] * k), int(r["H"] * k)), Image.LANCZOS)
            img.paste(l, (int(r["X"] * k), int(r["Y"] * k)), l)
    return img.resize((int(scene["Largeur"] * echelle), int(scene["Hauteur"] * echelle)), Image.LANCZOS)


if __name__ == "__main__":
    scenes = json.load(open(sys.argv[1], encoding="utf-8-sig"))
    out = sys.argv[2]
    logo = Image.open(sys.argv[3]).convert("RGBA") if len(sys.argv) > 3 else None
    echelle = float(sys.argv[4]) if len(sys.argv) > 4 else 1.5
    os.makedirs(out, exist_ok=True)
    for s in scenes:
        rendu(s, logo, echelle).save(os.path.join(out, s["Nom"] + ".png"))
        print(s["Nom"], s["Largeur"], "x", s["Hauteur"])
