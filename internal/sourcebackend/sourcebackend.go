package sourcebackend

import (
	"net/url"

	"github.com/Debian/dcs/ranking"
	"github.com/Debian/dcs/regexp"
)

func FilterByKeywords(rewritten *url.URL, files []ranking.ResultPath) []ranking.ResultPath {
	// The "package:" keyword, if specified.
	pkg := rewritten.Query().Get("package")
	// The "-package:" keywords, if specified.
	npkgs := rewritten.Query()["npackage"]
	// The "path:" keywords, if specified.
	paths := rewritten.Query()["path"]
	// The "-path" keywords, if specified.
	npaths := rewritten.Query()["npath"]

	// Filter the filenames if the "package:" keyword was specified.
	if pkg != "" {
		pkgRegexp, err := regexp.Compile(pkg)
		if err != nil {
			return files
		}
		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			if pkgRegexp.MatchString(file.Path[file.SourcePkgIdx[0]:file.SourcePkgIdx[1]], true, true) == -1 {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

	// Filter the filenames if the "-package:" keyword was specified.
	for _, npkg := range npkgs {
		npkgRegexp, err := regexp.Compile(npkg)
		if err != nil {
			return files
		}
		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			if npkgRegexp.MatchString(file.Path[file.SourcePkgIdx[0]:file.SourcePkgIdx[1]], true, true) != -1 {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

	for _, path := range paths {
		pathRegexp, err := regexp.Compile(path)
		if err != nil {
			return files
			// TODO: perform this validation before accepting the query, i.e. in dcs-web
			//err := common.Templates.ExecuteTemplate(w, "error.html", map[string]interface{}{
			//	"q":          r.URL.Query().Get("q"),
			//	"errormsg":   fmt.Sprintf(`%v`, err),
			//	"suggestion": template.HTML(`See <a href="http://codesearch.debian.net/faq#regexp">http://codesearch.debian.net/faq#regexp</a> for help on regular expressions.`),
			//})
			//if err != nil {
			//	http.Error(w, err.Error(), http.StatusInternalServerError)
			//}
		}

		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			if pathRegexp.MatchString(file.Path, true, true) == -1 {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

	for _, path := range npaths {
		pathRegexp, err := regexp.Compile(path)
		if err != nil {
			return files
			// TODO: perform this validation before accepting the query, i.e. in dcs-web
			//err := common.Templates.ExecuteTemplate(w, "error.html", map[string]interface{}{
			//	"q":          r.URL.Query().Get("q"),
			//	"errormsg":   fmt.Sprintf(`%v`, err),
			//	"suggestion": template.HTML(`See <a href="http://codesearch.debian.net/faq#regexp">http://codesearch.debian.net/faq#regexp</a> for help on regular expressions.`),
			//})
			//if err != nil {
			//	http.Error(w, err.Error(), http.StatusInternalServerError)
			//}
		}

		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			if pathRegexp.MatchString(file.Path, true, true) != -1 {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

	return files
}
