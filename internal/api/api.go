package api

type SearchResult struct {
	Package       string   `json:"package"`
	Path          string   `json:"path"`
	Line          uint32   `json:"line"`
	Context       string   `json:"context"`
	ContextBefore []string `json:"context_before,omitempty"`
	ContextAfter  []string `json:"context_after,omitempty"`
}

type PerPackageResult struct {
	Package string         `json:"package"`
	Results []SearchResult `json:"results"`
}
