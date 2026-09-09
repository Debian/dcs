# SearchResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Package** | **string** | The Debian source package containing this search result, including the full Debian version number. | 
**Path** | **string** | Path to the file containing the this search result, starting with &#x60;package&#x60;. | 
**Line** | **int32** | Line number containing the search result. | 
**ContextBefore** | Pointer to **[]string** | Up to 2 full lines before the search result (see &#x60;context&#x60;). | [optional] 
**Context** | **string** | The full line containing the search result. | 
**ContextAfter** | Pointer to **[]string** | Up to 2 full lines after the search result (see &#x60;context&#x60;). | [optional] 

## Methods

### NewSearchResult

`func NewSearchResult(package_ string, path string, line int32, context string, ) *SearchResult`

NewSearchResult instantiates a new SearchResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewSearchResultWithDefaults

`func NewSearchResultWithDefaults() *SearchResult`

NewSearchResultWithDefaults instantiates a new SearchResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPackage

`func (o *SearchResult) GetPackage() string`

GetPackage returns the Package field if non-nil, zero value otherwise.

### GetPackageOk

`func (o *SearchResult) GetPackageOk() (*string, bool)`

GetPackageOk returns a tuple with the Package field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPackage

`func (o *SearchResult) SetPackage(v string)`

SetPackage sets Package field to given value.


### GetPath

`func (o *SearchResult) GetPath() string`

GetPath returns the Path field if non-nil, zero value otherwise.

### GetPathOk

`func (o *SearchResult) GetPathOk() (*string, bool)`

GetPathOk returns a tuple with the Path field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPath

`func (o *SearchResult) SetPath(v string)`

SetPath sets Path field to given value.


### GetLine

`func (o *SearchResult) GetLine() int32`

GetLine returns the Line field if non-nil, zero value otherwise.

### GetLineOk

`func (o *SearchResult) GetLineOk() (*int32, bool)`

GetLineOk returns a tuple with the Line field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLine

`func (o *SearchResult) SetLine(v int32)`

SetLine sets Line field to given value.


### GetContextBefore

`func (o *SearchResult) GetContextBefore() []string`

GetContextBefore returns the ContextBefore field if non-nil, zero value otherwise.

### GetContextBeforeOk

`func (o *SearchResult) GetContextBeforeOk() (*[]string, bool)`

GetContextBeforeOk returns a tuple with the ContextBefore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetContextBefore

`func (o *SearchResult) SetContextBefore(v []string)`

SetContextBefore sets ContextBefore field to given value.

### HasContextBefore

`func (o *SearchResult) HasContextBefore() bool`

HasContextBefore returns a boolean if a field has been set.

### GetContext

`func (o *SearchResult) GetContext() string`

GetContext returns the Context field if non-nil, zero value otherwise.

### GetContextOk

`func (o *SearchResult) GetContextOk() (*string, bool)`

GetContextOk returns a tuple with the Context field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetContext

`func (o *SearchResult) SetContext(v string)`

SetContext sets Context field to given value.


### GetContextAfter

`func (o *SearchResult) GetContextAfter() []string`

GetContextAfter returns the ContextAfter field if non-nil, zero value otherwise.

### GetContextAfterOk

`func (o *SearchResult) GetContextAfterOk() (*[]string, bool)`

GetContextAfterOk returns a tuple with the ContextAfter field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetContextAfter

`func (o *SearchResult) SetContextAfter(v []string)`

SetContextAfter sets ContextAfter field to given value.

### HasContextAfter

`func (o *SearchResult) HasContextAfter() bool`

HasContextAfter returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


