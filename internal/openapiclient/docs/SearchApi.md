# \SearchApi

All URIs are relative to *https://codesearch.debian.net/api/v1*

Method | HTTP request | Description
------------- | ------------- | -------------
[**Search**](SearchApi.md#Search) | **Get** /search | Searches through source code
[**Searchperpackage**](SearchApi.md#Searchperpackage) | **Get** /searchperpackage | Like /search, but aggregates per package


# **Search**
> []SearchResult Search(ctx, query, optional)
Searches through source code

Performs a search through the full Debian Code Search corpus, blocking until all results are available (might take a few seconds depending on the search query).  Search results are ordered by their ranking (best results come first).

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **query** | **string**| The search query, for example &#x60;who knows...&#x60; (literal) or &#x60;who knows\\.\\.\\.&#x60; (regular expression). See https://codesearch.debian.net/faq for more details about which keywords are supported. The regular expression flavor is RE2, see https://github.com/google/re2/blob/master/doc/syntax.txt | 
 **optional** | ***SearchApiSearchOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a SearchApiSearchOpts struct

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------

 **matchMode** | **optional.String**| Whether the query is to be interpreted as a literal (&#x60;literal&#x60;) instead of as an RE2 regular expression (&#x60;regexp&#x60;). Literal searches are faster and do not require escaping special characters, regular expression searches are more powerful. | [default to regexp]

### Return type

[**[]SearchResult**](SearchResult.md)

### Authorization

[api_key](../README.md#api_key)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **Searchperpackage**
> []PackageSearchResult Searchperpackage(ctx, query, optional)
Like /search, but aggregates per package

The search results are currently sorted arbitrarily, but we intend to sort them by ranking eventually: https://github.com/Debian/dcs/blob/51338e934eb7ee18d00c5c18531c0790a83cb698/cmd/dcs-web/querymanager.go#L719

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **query** | **string**| The search query, for example &#x60;who knows...&#x60; (literal) or &#x60;who knows\\.\\.\\.&#x60; (regular expression). See https://codesearch.debian.net/faq for more details about which keywords are supported. The regular expression flavor is RE2, see https://github.com/google/re2/blob/master/doc/syntax.txt | 
 **optional** | ***SearchApiSearchperpackageOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a SearchApiSearchperpackageOpts struct

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------

 **matchMode** | **optional.String**| Whether the query is to be interpreted as a literal (&#x60;literal&#x60;) instead of as an RE2 regular expression (&#x60;regexp&#x60;). Literal searches are faster and do not require escaping special characters, regular expression searches are more powerful. | [default to regexp]

### Return type

[**[]PackageSearchResult**](PackageSearchResult.md)

### Authorization

[api_key](../README.md#api_key)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

