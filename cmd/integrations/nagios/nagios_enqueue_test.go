/*
Copyright © 2020 PagerDuty, Inc. <info@pagerduty.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package nagios

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/PagerDuty/go-pdagent/pkg/cmdutil"
	"github.com/PagerDuty/go-pdagent/test"
	"github.com/stretchr/testify/assert"
	"gopkg.in/h2non/gock.v1"
)

func buildCmdArgs(inputs nagiosEnqueueInput) []string {
	args := []string{}
	flags := []struct {
		flag string
		val  string
	}{
		{"-k", inputs.serviceKey}, {"-t", inputs.notificationType}, {"-n", inputs.sourceType}, {"-y", inputs.incidentKey},
	}
	for _, f := range flags {
		if f.val != "" {
			args = append(args, f.flag, f.val)
		}
	}
	for k, v := range inputs.customFields {
		args = append(args, "-f", fmt.Sprintf("%v=%v", k, v))
	}
	return args
}

func TestNagiosEnqueue_errors(t *testing.T) {
	tests := []struct {
		name          string
		inputs        nagiosEnqueueInput
		expectedError error
	}{
		{
			name:          "missingRequiredFlags",
			inputs:        nagiosEnqueueInput{},
			expectedError: errors.New("required flag(s) \"notification-type\", \"service-key\", \"source-type\" not set"),
		},
		{
			name: "invalidNotficationType",
			inputs: nagiosEnqueueInput{
				serviceKey:       "abc",
				notificationType: "trigger",
				sourceType:       "host",
			},
			expectedError: errNotificationType,
		},
		{
			name: "invalidSourceType",
			inputs: nagiosEnqueueInput{
				serviceKey:       "abc",
				notificationType: "PROBLEM",
				sourceType:       "invalidSourceType",
			},
			expectedError: errSourceType,
		},
		{
			name: "hostnameNotSetServiceCustomDetails",
			inputs: nagiosEnqueueInput{
				serviceKey:       "abc",
				notificationType: "RECOVERY",
				sourceType:       "service",
			},
			expectedError: errors.New("the HOSTNAME field must be set for source-type \"service\" using the -f flag"),
		},
		{
			name: "serviceDescNotSetServiceCustomDetails",
			inputs: nagiosEnqueueInput{
				serviceKey:       "abc",
				notificationType: "RECOVERY",
				sourceType:       "service",
				customFields: map[string]string{
					"HOSTNAME": "computer.network",
				},
			},
			expectedError: errors.New("the SERVICEDESC field must be set for source-type \"service\" using the -f flag"),
		},
		{
			name: "serviceStateNotSetServiceCustomDetails",
			inputs: nagiosEnqueueInput{
				serviceKey:       "abc",
				notificationType: "RECOVERY",
				sourceType:       "service",
				customFields: map[string]string{
					"HOSTNAME":    "computer.network",
					"SERVICEDESC": "a service",
				},
			},
			expectedError: errors.New("the SERVICESTATE field must be set for source-type \"service\" using the -f flag"),
		},
		{
			name: "hostnameNotSetHostCustomDetails",
			inputs: nagiosEnqueueInput{
				serviceKey:       "abc",
				notificationType: "RECOVERY",
				sourceType:       "host",
			},
			expectedError: errors.New("the HOSTNAME field must be set for source-type \"host\" using the -f flag"),
		},
		{
			name: "hoststateNotSetHostCustomDetails",
			inputs: nagiosEnqueueInput{
				serviceKey:       "abc",
				notificationType: "RECOVERY",
				sourceType:       "host",
				customFields: map[string]string{
					"HOSTNAME": "computer.network",
				},
			},
			expectedError: errors.New("the HOSTSTATE field must be set for source-type \"host\" using the -f flag"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.InitConfigForIntegrationsTesting()

			realConfig := cmdutil.NewConfig()

			cmd := NewNagiosEnqueueCmd(realConfig)
			cmd.SetArgs(buildCmdArgs(tt.inputs))

			_, err := cmd.ExecuteC()

			assert.Error(t, err)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}

func TestNagiosEnqueue_validInputs(t *testing.T) {
	tests := []struct {
		name      string
		cmdInputs nagiosEnqueueInput
	}{
		{
			name: "validSourceHostInput",
			cmdInputs: nagiosEnqueueInput{
				serviceKey:       "xyz",
				notificationType: "PROBLEM",
				sourceType:       "host",
				customFields: map[string]string{
					"HOSTNAME":  "computer.network",
					"HOSTSTATE": "down",
				},
			},
		},
		{
			name: "validSourceServiceInput",
			cmdInputs: nagiosEnqueueInput{
				serviceKey:       "xyz",
				notificationType: "PROBLEM",
				sourceType:       "service",
				customFields: map[string]string{
					"HOSTNAME":     "computer.network",
					"SERVICESTATE": "down",
					"SERVICEDESC":  "serviceA",
				},
			},
		},
		{
			name: "userProvidedIncidentKey",
			cmdInputs: nagiosEnqueueInput{
				serviceKey:       "xyz",
				notificationType: "PROBLEM",
				sourceType:       "service",
				incidentKey:      "someincidentkey",
				customFields: map[string]string{
					"HOSTNAME":     "computer.network",
					"SERVICESTATE": "down",
					"SERVICEDESC":  "serviceA",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.InitConfigForIntegrationsTesting()

			defer gock.Off()

			defaultHTTPClient := &http.Client{
				Timeout: 5 * time.Minute,
			}

			realConfig := cmdutil.NewConfig()
			realConfig.HttpClient = func() (*http.Client, error) {
				return defaultHTTPClient, nil
			}

			cmd := NewNagiosEnqueueCmd(realConfig)
			cmd.SetArgs(buildCmdArgs(tt.cmdInputs))

			incidentKey := tt.cmdInputs.incidentKey
			if incidentKey == "" {
				incidentKey = buildIncidentKey(tt.cmdInputs)
			}

			customDetails := map[string]string{
				"pd_nagios_object": tt.cmdInputs.sourceType,
			}
			for k, v := range tt.cmdInputs.customFields {
				customDetails[k] = v
			}

			expectedRequestBody := map[string]interface{}{
				"service_key":  tt.cmdInputs.serviceKey,
				"event_type":   nagiosToPagerDutyEventType[tt.cmdInputs.notificationType],
				"incident_key": incidentKey,
				"description":  buildEventDescription(tt.cmdInputs),
				"details":      customDetails,
			}

			gock.New(cmdutil.GetDefaults().Address).
				Post("/send").JSON(expectedRequestBody).
				Reply(200).JSON(map[string]interface{}{"key": tt.cmdInputs.serviceKey})

			gock.InterceptClient(defaultHTTPClient)

			out, err := test.CaptureStdout(func() error {
				_, err := cmd.ExecuteC()
				return err
			})

			if err != nil {
				t.Errorf("error running command `enqueue`: %v", err)
			}

			assert.Contains(t, out, fmt.Sprintf(`{"key":"%v"}`, tt.cmdInputs.serviceKey))
		})
	}
}
