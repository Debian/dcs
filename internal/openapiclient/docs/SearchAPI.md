# \SearchAPI

All URIs are relative to *https://codesearch.debian.net/api/v1*

Method | HTTP request | Description
------------- | ------------- | -------------
[**Search**](SearchAPI.md#Search) | **Get** /search | Searches through source code
[**Searchperpackage**](SearchAPI.md#Searchperpackage) | **Get** /searchperpackage | Like /search, but aggregates per package



## Search

> []SearchResult Search(ctx).Query(query).MatchMode(matchMode).Execute()

Searches through source code



### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	query := "query_example" // string | The search query, for example `who knows...` (literal) or `who knows\\.\\.\\.` (regular expression). See https://codesearch.debian.net/faq for more details about which keywords are supported. The regular expression flavor is RE2, see https://github.com/google/re2/blob/master/doc/syntax.txt
	matchMode := "matchMode_example" // string | Whether the query is to be interpreted as a literal (`literal`) instead of as an RE2 regular expression (`regexp`). Literal searches are faster and do not require escaping special characters, regular expression searches are more powerful. (optional) (default to "regexp")

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.SearchAPI.Search(context.Background()).Query(query).MatchMode(matchMode).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `SearchAPI.Search``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `Search`: []SearchResult
	fmt.Fprintf(os.Stdout, "Response from `SearchAPI.Search`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiSearchRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **query** | **string** | The search query, for example &#x60;who knows...&#x60; (literal) or &#x60;who knows\\.\\.\\.&#x60; (regular expression). See https://codesearch.debian.net/faq for more details about which keywords are supported. The regular expression flavor is RE2, see https://github.com/google/re2/blob/master/doc/syntax.txt | 
 **matchMode** | **string** | Whether the query is to be interpreted as a literal (&#x60;literal&#x60;) instead of as an RE2 regular expression (&#x60;regexp&#x60;). Literal searches are faster and do not require escaping special characters, regular expression searches are more powerful. | [default to &quot;regexp&quot;]

### Return type

[**[]SearchResult**](SearchResult.md)

### Authorization

[api_key](../README.md#api_key)

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## Searchperpackage

> []PackageSearchResult Searchperpackage(ctx).Query(query).MatchMode(matchMode).Execute()

Like /search, but aggregates per package



### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	query := "query_example" // string | The search query, for example `who knows...` (literal) or `who knows\\.\\.\\.` (regular expression). See https://codesearch.debian.net/faq for more details about which keywords are supported. The regular expression flavor is RE2, see https://github.com/google/re2/blob/master/doc/syntax.txt
	matchMode := "matchMode_example" // string | Whether the query is to be interpreted as a literal (`literal`) instead of as an RE2 regular expression (`regexp`). Literal searches are faster and do not require escaping special characters, regular expression searches are more powerful. (optional) (default to "regexp")

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.SearchAPI.Searchperpackage(context.Background()).Query(query).MatchMode(matchMode).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `SearchAPI.Searchperpackage``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `Searchperpackage`: []PackageSearchResult
	fmt.Fprintf(os.Stdout, "Response from `SearchAPI.Searchperpackage`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiSearchperpackageRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **query** | **string** | The search query, for example &#x60;who knows...&#x60; (literal) or &#x60;who knows\\.\\.\\.&#x60; (regular expression). See https://codesearch.debian.net/faq for more details about which keywords are supported. The regular expression flavor is RE2, see https://github.com/google/re2/blob/master/doc/syntax.txt | 
 **matchMode** | **string** | Whether the query is to be interpreted as a literal (&#x60;literal&#x60;) instead of as an RE2 regular expression (&#x60;regexp&#x60;). Literal searches are faster and do not require escaping special characters, regular expression searches are more powerful. | [default to &quot;regexp&quot;]

### Return type

[**[]PackageSearchResult**](PackageSearchResult.md)

### Authorization

[api_key](../README.md#api_key)

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)

