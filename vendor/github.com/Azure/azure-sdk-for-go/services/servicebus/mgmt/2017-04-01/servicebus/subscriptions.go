package servicebus

// Copyright (c) Microsoft and contributors.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Code generated by Microsoft (R) AutoRest Code Generator.
// Changes may cause incorrect behavior and will be lost if the code is regenerated.

import (
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/validation"
	"net/http"
)

// SubscriptionsClient is the azure Service Bus client
type SubscriptionsClient struct {
	ManagementClient
}

// NewSubscriptionsClient creates an instance of the SubscriptionsClient client.
func NewSubscriptionsClient(subscriptionID string) SubscriptionsClient {
	return NewSubscriptionsClientWithBaseURI(DefaultBaseURI, subscriptionID)
}

// NewSubscriptionsClientWithBaseURI creates an instance of the SubscriptionsClient client.
func NewSubscriptionsClientWithBaseURI(baseURI string, subscriptionID string) SubscriptionsClient {
	return SubscriptionsClient{NewWithBaseURI(baseURI, subscriptionID)}
}

// CreateOrUpdate creates a topic subscription.
//
// resourceGroupName is name of the Resource group within the Azure subscription. namespaceName is the namespace name
// topicName is the topic name. subscriptionName is the subscription name. parameters is parameters supplied to create
// a subscription resource.
func (client SubscriptionsClient) CreateOrUpdate(resourceGroupName string, namespaceName string, topicName string, subscriptionName string, parameters SBSubscription) (result SBSubscription, err error) {
	if err := validation.Validate([]validation.Validation{
		{TargetValue: resourceGroupName,
			Constraints: []validation.Constraint{{Target: "resourceGroupName", Name: validation.MaxLength, Rule: 90, Chain: nil},
				{Target: "resourceGroupName", Name: validation.MinLength, Rule: 1, Chain: nil}}},
		{TargetValue: namespaceName,
			Constraints: []validation.Constraint{{Target: "namespaceName", Name: validation.MaxLength, Rule: 50, Chain: nil},
				{Target: "namespaceName", Name: validation.MinLength, Rule: 6, Chain: nil}}},
		{TargetValue: topicName,
			Constraints: []validation.Constraint{{Target: "topicName", Name: validation.MinLength, Rule: 1, Chain: nil}}},
		{TargetValue: subscriptionName,
			Constraints: []validation.Constraint{{Target: "subscriptionName", Name: validation.MaxLength, Rule: 50, Chain: nil},
				{Target: "subscriptionName", Name: validation.MinLength, Rule: 1, Chain: nil}}}}); err != nil {
		return result, validation.NewErrorWithValidationError(err, "servicebus.SubscriptionsClient", "CreateOrUpdate")
	}

	req, err := client.CreateOrUpdatePreparer(resourceGroupName, namespaceName, topicName, subscriptionName, parameters)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "CreateOrUpdate", nil, "Failure preparing request")
		return
	}

	resp, err := client.CreateOrUpdateSender(req)
	if err != nil {
		result.Response = autorest.Response{Response: resp}
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "CreateOrUpdate", resp, "Failure sending request")
		return
	}

	result, err = client.CreateOrUpdateResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "CreateOrUpdate", resp, "Failure responding to request")
	}

	return
}

// CreateOrUpdatePreparer prepares the CreateOrUpdate request.
func (client SubscriptionsClient) CreateOrUpdatePreparer(resourceGroupName string, namespaceName string, topicName string, subscriptionName string, parameters SBSubscription) (*http.Request, error) {
	pathParameters := map[string]interface{}{
		"namespaceName":     autorest.Encode("path", namespaceName),
		"resourceGroupName": autorest.Encode("path", resourceGroupName),
		"subscriptionId":    autorest.Encode("path", client.SubscriptionID),
		"subscriptionName":  autorest.Encode("path", subscriptionName),
		"topicName":         autorest.Encode("path", topicName),
	}

	const APIVersion = "2017-04-01"
	queryParameters := map[string]interface{}{
		"api-version": APIVersion,
	}

	preparer := autorest.CreatePreparer(
		autorest.AsJSON(),
		autorest.AsPut(),
		autorest.WithBaseURL(client.BaseURI),
		autorest.WithPathParameters("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions/{subscriptionName}", pathParameters),
		autorest.WithJSON(parameters),
		autorest.WithQueryParameters(queryParameters))
	return preparer.Prepare(&http.Request{})
}

// CreateOrUpdateSender sends the CreateOrUpdate request. The method will close the
// http.Response Body if it receives an error.
func (client SubscriptionsClient) CreateOrUpdateSender(req *http.Request) (*http.Response, error) {
	return autorest.SendWithSender(client,
		req,
		azure.DoRetryWithRegistration(client.Client))
}

// CreateOrUpdateResponder handles the response to the CreateOrUpdate request. The method always
// closes the http.Response Body.
func (client SubscriptionsClient) CreateOrUpdateResponder(resp *http.Response) (result SBSubscription, err error) {
	err = autorest.Respond(
		resp,
		client.ByInspecting(),
		azure.WithErrorUnlessStatusCode(http.StatusOK),
		autorest.ByUnmarshallingJSON(&result),
		autorest.ByClosing())
	result.Response = autorest.Response{Response: resp}
	return
}

// Delete deletes a subscription from the specified topic.
//
// resourceGroupName is name of the Resource group within the Azure subscription. namespaceName is the namespace name
// topicName is the topic name. subscriptionName is the subscription name.
func (client SubscriptionsClient) Delete(resourceGroupName string, namespaceName string, topicName string, subscriptionName string) (result autorest.Response, err error) {
	if err := validation.Validate([]validation.Validation{
		{TargetValue: resourceGroupName,
			Constraints: []validation.Constraint{{Target: "resourceGroupName", Name: validation.MaxLength, Rule: 90, Chain: nil},
				{Target: "resourceGroupName", Name: validation.MinLength, Rule: 1, Chain: nil}}},
		{TargetValue: namespaceName,
			Constraints: []validation.Constraint{{Target: "namespaceName", Name: validation.MaxLength, Rule: 50, Chain: nil},
				{Target: "namespaceName", Name: validation.MinLength, Rule: 6, Chain: nil}}},
		{TargetValue: topicName,
			Constraints: []validation.Constraint{{Target: "topicName", Name: validation.MinLength, Rule: 1, Chain: nil}}},
		{TargetValue: subscriptionName,
			Constraints: []validation.Constraint{{Target: "subscriptionName", Name: validation.MaxLength, Rule: 50, Chain: nil},
				{Target: "subscriptionName", Name: validation.MinLength, Rule: 1, Chain: nil}}}}); err != nil {
		return result, validation.NewErrorWithValidationError(err, "servicebus.SubscriptionsClient", "Delete")
	}

	req, err := client.DeletePreparer(resourceGroupName, namespaceName, topicName, subscriptionName)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "Delete", nil, "Failure preparing request")
		return
	}

	resp, err := client.DeleteSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "Delete", resp, "Failure sending request")
		return
	}

	result, err = client.DeleteResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "Delete", resp, "Failure responding to request")
	}

	return
}

// DeletePreparer prepares the Delete request.
func (client SubscriptionsClient) DeletePreparer(resourceGroupName string, namespaceName string, topicName string, subscriptionName string) (*http.Request, error) {
	pathParameters := map[string]interface{}{
		"namespaceName":     autorest.Encode("path", namespaceName),
		"resourceGroupName": autorest.Encode("path", resourceGroupName),
		"subscriptionId":    autorest.Encode("path", client.SubscriptionID),
		"subscriptionName":  autorest.Encode("path", subscriptionName),
		"topicName":         autorest.Encode("path", topicName),
	}

	const APIVersion = "2017-04-01"
	queryParameters := map[string]interface{}{
		"api-version": APIVersion,
	}

	preparer := autorest.CreatePreparer(
		autorest.AsDelete(),
		autorest.WithBaseURL(client.BaseURI),
		autorest.WithPathParameters("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions/{subscriptionName}", pathParameters),
		autorest.WithQueryParameters(queryParameters))
	return preparer.Prepare(&http.Request{})
}

// DeleteSender sends the Delete request. The method will close the
// http.Response Body if it receives an error.
func (client SubscriptionsClient) DeleteSender(req *http.Request) (*http.Response, error) {
	return autorest.SendWithSender(client,
		req,
		azure.DoRetryWithRegistration(client.Client))
}

// DeleteResponder handles the response to the Delete request. The method always
// closes the http.Response Body.
func (client SubscriptionsClient) DeleteResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		client.ByInspecting(),
		azure.WithErrorUnlessStatusCode(http.StatusNoContent, http.StatusOK),
		autorest.ByClosing())
	result.Response = resp
	return
}

// Get returns a subscription description for the specified topic.
//
// resourceGroupName is name of the Resource group within the Azure subscription. namespaceName is the namespace name
// topicName is the topic name. subscriptionName is the subscription name.
func (client SubscriptionsClient) Get(resourceGroupName string, namespaceName string, topicName string, subscriptionName string) (result SBSubscription, err error) {
	if err := validation.Validate([]validation.Validation{
		{TargetValue: resourceGroupName,
			Constraints: []validation.Constraint{{Target: "resourceGroupName", Name: validation.MaxLength, Rule: 90, Chain: nil},
				{Target: "resourceGroupName", Name: validation.MinLength, Rule: 1, Chain: nil}}},
		{TargetValue: namespaceName,
			Constraints: []validation.Constraint{{Target: "namespaceName", Name: validation.MaxLength, Rule: 50, Chain: nil},
				{Target: "namespaceName", Name: validation.MinLength, Rule: 6, Chain: nil}}},
		{TargetValue: topicName,
			Constraints: []validation.Constraint{{Target: "topicName", Name: validation.MinLength, Rule: 1, Chain: nil}}},
		{TargetValue: subscriptionName,
			Constraints: []validation.Constraint{{Target: "subscriptionName", Name: validation.MaxLength, Rule: 50, Chain: nil},
				{Target: "subscriptionName", Name: validation.MinLength, Rule: 1, Chain: nil}}}}); err != nil {
		return result, validation.NewErrorWithValidationError(err, "servicebus.SubscriptionsClient", "Get")
	}

	req, err := client.GetPreparer(resourceGroupName, namespaceName, topicName, subscriptionName)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "Get", nil, "Failure preparing request")
		return
	}

	resp, err := client.GetSender(req)
	if err != nil {
		result.Response = autorest.Response{Response: resp}
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "Get", resp, "Failure sending request")
		return
	}

	result, err = client.GetResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "Get", resp, "Failure responding to request")
	}

	return
}

// GetPreparer prepares the Get request.
func (client SubscriptionsClient) GetPreparer(resourceGroupName string, namespaceName string, topicName string, subscriptionName string) (*http.Request, error) {
	pathParameters := map[string]interface{}{
		"namespaceName":     autorest.Encode("path", namespaceName),
		"resourceGroupName": autorest.Encode("path", resourceGroupName),
		"subscriptionId":    autorest.Encode("path", client.SubscriptionID),
		"subscriptionName":  autorest.Encode("path", subscriptionName),
		"topicName":         autorest.Encode("path", topicName),
	}

	const APIVersion = "2017-04-01"
	queryParameters := map[string]interface{}{
		"api-version": APIVersion,
	}

	preparer := autorest.CreatePreparer(
		autorest.AsGet(),
		autorest.WithBaseURL(client.BaseURI),
		autorest.WithPathParameters("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions/{subscriptionName}", pathParameters),
		autorest.WithQueryParameters(queryParameters))
	return preparer.Prepare(&http.Request{})
}

// GetSender sends the Get request. The method will close the
// http.Response Body if it receives an error.
func (client SubscriptionsClient) GetSender(req *http.Request) (*http.Response, error) {
	return autorest.SendWithSender(client,
		req,
		azure.DoRetryWithRegistration(client.Client))
}

// GetResponder handles the response to the Get request. The method always
// closes the http.Response Body.
func (client SubscriptionsClient) GetResponder(resp *http.Response) (result SBSubscription, err error) {
	err = autorest.Respond(
		resp,
		client.ByInspecting(),
		azure.WithErrorUnlessStatusCode(http.StatusOK),
		autorest.ByUnmarshallingJSON(&result),
		autorest.ByClosing())
	result.Response = autorest.Response{Response: resp}
	return
}

// ListByTopic list all the subscriptions under a specified topic.
//
// resourceGroupName is name of the Resource group within the Azure subscription. namespaceName is the namespace name
// topicName is the topic name.
func (client SubscriptionsClient) ListByTopic(resourceGroupName string, namespaceName string, topicName string) (result SBSubscriptionListResult, err error) {
	if err := validation.Validate([]validation.Validation{
		{TargetValue: resourceGroupName,
			Constraints: []validation.Constraint{{Target: "resourceGroupName", Name: validation.MaxLength, Rule: 90, Chain: nil},
				{Target: "resourceGroupName", Name: validation.MinLength, Rule: 1, Chain: nil}}},
		{TargetValue: namespaceName,
			Constraints: []validation.Constraint{{Target: "namespaceName", Name: validation.MaxLength, Rule: 50, Chain: nil},
				{Target: "namespaceName", Name: validation.MinLength, Rule: 6, Chain: nil}}},
		{TargetValue: topicName,
			Constraints: []validation.Constraint{{Target: "topicName", Name: validation.MinLength, Rule: 1, Chain: nil}}}}); err != nil {
		return result, validation.NewErrorWithValidationError(err, "servicebus.SubscriptionsClient", "ListByTopic")
	}

	req, err := client.ListByTopicPreparer(resourceGroupName, namespaceName, topicName)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "ListByTopic", nil, "Failure preparing request")
		return
	}

	resp, err := client.ListByTopicSender(req)
	if err != nil {
		result.Response = autorest.Response{Response: resp}
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "ListByTopic", resp, "Failure sending request")
		return
	}

	result, err = client.ListByTopicResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "ListByTopic", resp, "Failure responding to request")
	}

	return
}

// ListByTopicPreparer prepares the ListByTopic request.
func (client SubscriptionsClient) ListByTopicPreparer(resourceGroupName string, namespaceName string, topicName string) (*http.Request, error) {
	pathParameters := map[string]interface{}{
		"namespaceName":     autorest.Encode("path", namespaceName),
		"resourceGroupName": autorest.Encode("path", resourceGroupName),
		"subscriptionId":    autorest.Encode("path", client.SubscriptionID),
		"topicName":         autorest.Encode("path", topicName),
	}

	const APIVersion = "2017-04-01"
	queryParameters := map[string]interface{}{
		"api-version": APIVersion,
	}

	preparer := autorest.CreatePreparer(
		autorest.AsGet(),
		autorest.WithBaseURL(client.BaseURI),
		autorest.WithPathParameters("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions", pathParameters),
		autorest.WithQueryParameters(queryParameters))
	return preparer.Prepare(&http.Request{})
}

// ListByTopicSender sends the ListByTopic request. The method will close the
// http.Response Body if it receives an error.
func (client SubscriptionsClient) ListByTopicSender(req *http.Request) (*http.Response, error) {
	return autorest.SendWithSender(client,
		req,
		azure.DoRetryWithRegistration(client.Client))
}

// ListByTopicResponder handles the response to the ListByTopic request. The method always
// closes the http.Response Body.
func (client SubscriptionsClient) ListByTopicResponder(resp *http.Response) (result SBSubscriptionListResult, err error) {
	err = autorest.Respond(
		resp,
		client.ByInspecting(),
		azure.WithErrorUnlessStatusCode(http.StatusOK),
		autorest.ByUnmarshallingJSON(&result),
		autorest.ByClosing())
	result.Response = autorest.Response{Response: resp}
	return
}

// ListByTopicNextResults retrieves the next set of results, if any.
func (client SubscriptionsClient) ListByTopicNextResults(lastResults SBSubscriptionListResult) (result SBSubscriptionListResult, err error) {
	req, err := lastResults.SBSubscriptionListResultPreparer()
	if err != nil {
		return result, autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "ListByTopic", nil, "Failure preparing next results request")
	}
	if req == nil {
		return
	}

	resp, err := client.ListByTopicSender(req)
	if err != nil {
		result.Response = autorest.Response{Response: resp}
		return result, autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "ListByTopic", resp, "Failure sending next results request")
	}

	result, err = client.ListByTopicResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "servicebus.SubscriptionsClient", "ListByTopic", resp, "Failure responding to next results request")
	}

	return
}

// ListByTopicComplete gets all elements from the list without paging.
func (client SubscriptionsClient) ListByTopicComplete(resourceGroupName string, namespaceName string, topicName string, cancel <-chan struct{}) (<-chan SBSubscription, <-chan error) {
	resultChan := make(chan SBSubscription)
	errChan := make(chan error, 1)
	go func() {
		defer func() {
			close(resultChan)
			close(errChan)
		}()
		list, err := client.ListByTopic(resourceGroupName, namespaceName, topicName)
		if err != nil {
			errChan <- err
			return
		}
		if list.Value != nil {
			for _, item := range *list.Value {
				select {
				case <-cancel:
					return
				case resultChan <- item:
					// Intentionally left blank
				}
			}
		}
		for list.NextLink != nil {
			list, err = client.ListByTopicNextResults(list)
			if err != nil {
				errChan <- err
				return
			}
			if list.Value != nil {
				for _, item := range *list.Value {
					select {
					case <-cancel:
						return
					case resultChan <- item:
						// Intentionally left blank
					}
				}
			}
		}
	}()
	return resultChan, errChan
}
