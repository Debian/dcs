// vim:ts=4:sw=4:noexpandtab
package index

import (
	"net/http"
	"html/template"
)

var templates = template.Must(template.ParseFiles("templates/index.html"))

func Index(w http.ResponseWriter, r *http.Request) {
	err := templates.ExecuteTemplate(w, "index.html", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
