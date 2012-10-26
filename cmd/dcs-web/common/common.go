// vim:ts=4:sw=4:noexpandtab

// common flags for dcs-web
package common

import (
	"flag"
	"html/template"
	"log"
	"reflect"
)

var templatePattern = flag.String("template_pattern",
	"templates/*",
	"Pattern matching the HTML templates (./templates/* by default)")
var SourceBackends = flag.String("source_backends",
	"localhost:28082",
	"host:port (multiple values are comma-separated) of the source-backend(s)")
var Templates *template.Template

func LoadTemplates() {
	var err error
	Templates = template.New("foo").Funcs(template.FuncMap{
		"eq": func(args ...interface{}) bool {
			if len(args) == 0 {
				return false
			}
			x := args[0]
			switch x := x.(type) {
			case string, int, int64, byte, float32, float64:
				for _, y := range args[1:] {
					if x == y {
						return true
					}
				}
				return false
			}

			for _, y := range args[1:] {
				if reflect.DeepEqual(x, y) {
					return true
				}
			}
			return false
		},
	})
	Templates, err = Templates.ParseGlob(*templatePattern)
	if err != nil {
		log.Fatalf(`Could not load templates from "%s": %v`, *templatePattern, err)
	}
}
