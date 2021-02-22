# SearchResult

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Package_** | **string** | The Debian source package containing this search result, including the full Debian version number. | [default to null]
**Path** | **string** | Path to the file containing the this search result, relative to &#x60;package&#x60;. | [default to null]
**Line** | **int32** | Line number containing the search result. | [default to null]
**ContextBefore** | **[]string** | Up to 2 full lines before the search result (see &#x60;context&#x60;). | [optional] [default to null]
**Context** | **string** | The full line containing the search result. | [default to null]
**ContextAfter** | **[]string** | Up to 2 full lines after the search result (see &#x60;context&#x60;). | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


