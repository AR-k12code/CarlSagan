package cognos

import (
	"encoding/xml"
	"strings"

	"github.com/9072997/jgh"
	"github.com/antchfx/xmlquery"
)

// used to construct a XML response with our prompt values
type promptAnswers struct {
	XMLName      xml.Name      `xml:"promptAnswers"`
	PromptValues []promptValue `xml:"promptValues"`
}
type promptValue struct {
	Name   string `xml:"name"`
	Values struct {
		Item struct {
			SimplePValue struct {
				Inclusive string `xml:"inclusive"`
				Value     string `xml:"useValue"`
			} `xml:"SimplePValue"`
		} `xml:"item"`
	} `xml:"values"`
}

// take a map of [parameter name] -> [value] and return XML in the format
// cognos wants (but not yet URL encoded)
func makeAnswersXML(values map[string]string) string {
	var a promptAnswers
	for name, value := range values {
		var v promptValue
		v.Name = name
		v.Values.Item.SimplePValue.Value = value
		// is this ever false?
		v.Values.Item.SimplePValue.Inclusive = "true"

		a.PromptValues = append(a.PromptValues, v)
	}

	answersXML, err := xml.Marshal(a)
	jgh.PanicOnErr(err)
	return string(answersXML)
}

// operates in-place
func removeNamespaces(n *xmlquery.Node) {
	n.Prefix = ""
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		removeNamespaces(child)
	}
}

// ListReportPrompts returns an array of option names given the path to a
// report
func (c Session) ListReportPrompts(path []string) []string {
	// ask cognos to list options
	url := "/ibmcognos/bi/v1/disp/rds/reportPrompts/path/" +
		c.encodePath(path)
	optionsXML := c.Request("GET", url, "")

	// parse XML
	optionsDoc, err := xmlquery.Parse(strings.NewReader(optionsXML))
	jgh.PanicOnErr(err)

	// remove all namespace information from the document
	removeNamespaces(optionsDoc)

	// get all <pname> elements
	optionNodes := xmlquery.Find(optionsDoc, "//pname")

	// get inner text from each node
	var options []string
	for _, node := range optionNodes {
		options = append(options, node.InnerText())
	}

	return options
}
