//go:build ignore

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/google/renameio/v2"
)

func minify() error {
	for _, js := range []string{
		"instant.js",
	} {
		min := strings.TrimSuffix(js, ".js") + ".min.js"
		b, err := os.ReadFile(js)
		if err != nil {
			return err
		}
		res := api.Transform(string(b), api.TransformOptions{
			Loader:            api.LoaderJS,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      true,
		})
		if len(res.Errors) > 0 {
			return fmt.Errorf("esbuild.minify(%s): %v", min, res.Errors)
		}
		if err := renameio.WriteFile(min, res.Code, 0644); err != nil {
			return err
		}
	}

	for _, css := range []string{
		"critical.css",
		"non-critical.css",
	} {
		min := strings.TrimSuffix(css, ".css") + ".min.css"
		b, err := os.ReadFile(css)
		if err != nil {
			return err
		}
		res := api.Transform(string(b), api.TransformOptions{
			Loader:            api.LoaderCSS,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      true,
		})
		if len(res.Errors) > 0 {
			return fmt.Errorf("esbuild.minify(%s): %v", min, res.Errors)
		}
		if err := renameio.WriteFile(min, res.Code, 0644); err != nil {
			return err
		}
	}

	// Concatenate debian.css and debcodesearch.css to debcodesearch.min.css.
	{
		const min = "debcodesearch.min.css"
		debianCSS, err := os.ReadFile("debian.css")
		if err != nil {
			return err
		}
		dcsCSS, err := os.ReadFile("debcodesearch.css")
		if err != nil {
			return err
		}
		res := api.Transform(string(append(debianCSS, dcsCSS...)), api.TransformOptions{
			Loader:            api.LoaderCSS,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      true,
		})
		if len(res.Errors) > 0 {
			return fmt.Errorf("esbuild.minify(%s): %v", min, res.Errors)
		}
		if err := renameio.WriteFile(min, res.Code, 0644); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	flag.Parse()
	if err := minify(); err != nil {
		log.Fatal(err)
	}
}
