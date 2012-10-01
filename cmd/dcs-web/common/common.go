// vim:ts=4:sw=4:noexpandtab

// common flags for dcs-web
package common

import (
	"flag"
	"html/template"
	"log"
)

var templatePattern = flag.String("template_pattern",
	"templates/*",
	"Pattern matching the HTML templates (./templates/* by default)")
var Templates *template.Template

func LoadTemplates() {
	var err error
	Templates, err = template.ParseGlob(*templatePattern)
	if err != nil {
		log.Fatalf(`Could not load templates from "%s": %v`, *templatePattern, err)
	}
}
