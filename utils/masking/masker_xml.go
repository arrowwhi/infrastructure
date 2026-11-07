package masking

import (
	"errors"

	"github.com/beevik/etree"
)

func (m *Masker) maskXML(data []byte) ([]byte, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, err
	}
	doc.WriteSettings.CanonicalEndTags = true

	root := doc.Root()
	if root == nil {
		return nil, errors.New("XML document is empty")
	}
	m.maskXMLRecursively(root)

	return doc.WriteToBytes()
}

func (m *Masker) maskXMLRecursively(elem *etree.Element) {
	if text := elem.Text(); text != "" {
		elem.SetText(m.maskXMLElement(elem.Tag, text))
	}
	for i, attr := range elem.Attr {
		elem.Attr[i].Value = m.maskXMLElement(attr.Key, attr.Value)
	}
	for _, child := range elem.ChildElements() {
		m.maskXMLRecursively(child)
	}
}

func (m *Masker) maskXMLElement(key, val string) string {
	if r := m.findMatchingRule(key, m.keyRules); r != nil {
		return string(r.maskFn([]byte(val)))
	}
	if r := m.findMatchingRule(val, m.valRules); r != nil {
		return string(r.maskFn([]byte(val)))
	}
	return val
}

func (m *Masker) findMatchingRule(s string, rules []rule) *rule {
	for i := range rules {
		for j := range rules[i].froms {
			if rules[i].froms[j].MatchString(s) {
				return &rules[i]
			}
		}
	}
	return nil
}
