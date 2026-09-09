# PackageSearchResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Package** | **string** | The Debian source package for which up to 2 search results have been aggregated in &#x60;results&#x60;. | 
**Results** | [**[]SearchResult**](SearchResult.md) |  | 

## Methods

### NewPackageSearchResult

`func NewPackageSearchResult(package_ string, results []SearchResult, ) *PackageSearchResult`

NewPackageSearchResult instantiates a new PackageSearchResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPackageSearchResultWithDefaults

`func NewPackageSearchResultWithDefaults() *PackageSearchResult`

NewPackageSearchResultWithDefaults instantiates a new PackageSearchResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPackage

`func (o *PackageSearchResult) GetPackage() string`

GetPackage returns the Package field if non-nil, zero value otherwise.

### GetPackageOk

`func (o *PackageSearchResult) GetPackageOk() (*string, bool)`

GetPackageOk returns a tuple with the Package field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPackage

`func (o *PackageSearchResult) SetPackage(v string)`

SetPackage sets Package field to given value.


### GetResults

`func (o *PackageSearchResult) GetResults() []SearchResult`

GetResults returns the Results field if non-nil, zero value otherwise.

### GetResultsOk

`func (o *PackageSearchResult) GetResultsOk() (*[]SearchResult, bool)`

GetResultsOk returns a tuple with the Results field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetResults

`func (o *PackageSearchResult) SetResults(v []SearchResult)`

SetResults sets Results field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


