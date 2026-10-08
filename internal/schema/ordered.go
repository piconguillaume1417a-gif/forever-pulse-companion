package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// KV / Obj : objet JSON à ordre fixe, pour des corps d'envoi lisibles et stables.
type KV struct {
	K string
	V any
}

type Obj []KV

func (o Obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.K)
		b.Write(k)
		b.WriteByte(':')
		v, err := marshal(kv.V)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal sans échappement HTML : les noms de personnages gardent < > & tels quels.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// Marshal : JSON compact sans échappement HTML.
func Marshal(v any) ([]byte, error) { return marshal(v) }

// Get renvoie la valeur d'une clé (nil si absente).
func (o Obj) Get(k string) any {
	for _, kv := range o {
		if kv.K == k {
			return kv.V
		}
	}
	return nil
}

// Membre : un membre d'objet JSON gardé tel qu'il a été écrit (clé décodée, valeur brute).
type Membre struct {
	K string
	V json.RawMessage
}

// Membres découpe un objet JSON en ses membres, dans l'ordre, sans réécrire les
// valeurs : un corps assemblé de nouveau à partir des mêmes membres est identique
// octet pour octet à l'original compact.
func Membres(obj []byte) ([]Membre, error) {
	d := json.NewDecoder(bytes.NewReader(obj))
	d.UseNumber()
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if t != json.Delim('{') {
		return nil, errors.New("objet JSON attendu")
	}
	var out []Membre
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		k, ok := t.(string)
		if !ok {
			return nil, errors.New("clé JSON attendue")
		}
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, Membre{K: k, V: v})
	}
	if t, err := d.Token(); err != nil || t != json.Delim('}') {
		return nil, errors.New("objet JSON non fermé")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("données après l'objet JSON")
	}
	return out, nil
}

// Assemble réécrit un objet JSON compact à partir de ses membres.
func Assemble(ms []Membre) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := marshal(m.K)
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.V)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// SansCles retire des membres d'un objet JSON compact ; les autres restent
// identiques octet pour octet. Rien à retirer : l'objet est rendu tel quel.
func SansCles(obj []byte, cles ...string) ([]byte, error) {
	ms, err := Membres(obj)
	if err != nil {
		return nil, err
	}
	garde := ms[:0:0]
	for _, m := range ms {
		retire := false
		for _, c := range cles {
			if m.K == c {
				retire = true
			}
		}
		if !retire {
			garde = append(garde, m)
		}
	}
	if len(garde) == len(ms) {
		return obj, nil
	}
	return Assemble(garde), nil
}
