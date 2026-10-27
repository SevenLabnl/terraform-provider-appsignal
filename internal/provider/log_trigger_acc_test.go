package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

func logTriggerExists(appID *string, id *string, want bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if *appID == "" {
			*appID = appIDOf(s)
		}
		_, err := testClient().GetLogTrigger(context.Background(), *appID, *id)
		switch {
		case err == nil && !want:
			return fmt.Errorf("log trigger %s still exists", *id)
		case client.IsNotFound(err) && want:
			return fmt.Errorf("log trigger %s does not exist", *id)
		case err != nil && !client.IsNotFound(err):
			return err
		}
		return nil
	}
}

const logTriggerFull = `
resource "appsignal_log_source" "test" {
  app_id = appsignal_app.test.id
  name   = "acc-source"
  type   = "http_json"
}

resource "appsignal_log_trigger" "test" {
  app_id                 = appsignal_app.test.id
  name                   = %q
  query                  = "severity:error"
  description            = "errors in the importer"
  source_ids             = [appsignal_log_source.test.id]
  notifier_ids           = [data.appsignal_notifier.email.id]
  notification_frequency = "nth_in_hour"
  notification_threshold = 3
}
`

const logTriggerMinimal = `
resource "appsignal_log_trigger" "test" {
  app_id = appsignal_app.test.id
  name   = "importer errors"
  query  = "severity:fatal"
}
`

func TestAccLogTrigger(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_log_trigger.test"
	var appID, first, second string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: appConfig(name) + fmt.Sprintf(logTriggerFull, "importer errors"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "importer errors"),
					resource.TestCheckResourceAttr(addr, "query", "severity:error"),
					resource.TestCheckResourceAttr(addr, "description", "errors in the importer"),
					resource.TestCheckResourceAttr(addr, "source_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "source_ids.0", "appsignal_log_source.test", "id"),
					resource.TestCheckResourceAttr(addr, "notifier_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "notifier_ids.0", "data.appsignal_notifier.email", "id"),
					resource.TestCheckResourceAttr(addr, "notification_frequency", "nth_in_hour"),
					resource.TestCheckResourceAttr(addr, "notification_threshold", "3"),
					saveID(addr, &first),
					logTriggerExists(&appID, &first, true),
				),
			},
			{
				// Log triggers change in place.
				Config: appConfig(name) + fmt.Sprintf(logTriggerFull, "importer failures"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "importer failures"),
					idIs(addr, &first, true),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", addr),
			},
			{
				// Removing the optional arguments clears them in AppSignal, which keeps
				// any argument an update leaves out.
				Config: appConfig(name) + logTriggerMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckResourceAttr(addr, "query", "severity:fatal"),
					resource.TestCheckNoResourceAttr(addr, "description"),
					resource.TestCheckResourceAttr(addr, "source_ids.#", "0"),
					resource.TestCheckResourceAttr(addr, "notifier_ids.#", "0"),
					resource.TestCheckResourceAttr(addr, "notification_frequency", "always"),
					resource.TestCheckResourceAttr(addr, "notification_threshold", "1"),
					func(s *terraform.State) error {
						lt, err := testClient().GetLogTrigger(context.Background(), appID, first)
						if err != nil {
							return err
						}
						if (lt.Description != nil && *lt.Description != "") || len(lt.SourceIDs) != 0 || len(lt.Notifiers) != 0 ||
							lt.NotificationOptions != "ALWAYS" || lt.NotificationTriggerValue != 1 {
							return fmt.Errorf("AppSignal kept old values: %+v", lt)
						}
						return nil
					},
				),
			},
			{
				// A log trigger deleted outside tofu is created again.
				PreConfig: func() {
					if err := testClient().DeleteLogTrigger(context.Background(), appID, first); err != nil {
						t.Fatal(err)
					}
				},
				Config: appConfig(name) + logTriggerMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, false),
					saveID(addr, &second),
					logTriggerExists(&appID, &second, true),
				),
			},
			{
				Config: appConfig(name),
				Check:  logTriggerExists(&appID, &second, false),
			},
		},
	})
}

func TestLogTriggerValidation(t *testing.T) {
	cases := map[string]struct {
		set map[string]string
		err string
	}{
		"unknown frequency": {map[string]string{"notification_frequency": `"hourly"`}, `notification_frequency value must be one of`},
		"zero threshold":    {map[string]string{"notification_threshold": "0"}, `notification_threshold value must be at least 1`},
		"empty name":        {map[string]string{"name": `""`}, `name string length must be at least 1`},
		"empty query":       {map[string]string{"query": `""`}, `query string length must be at least 1`},
		"empty description": {map[string]string{"description": `""`}, `description string length must be at least 1`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			attrs := map[string]string{"app_id": `"x"`, "name": `"n"`, "query": `"q"`}
			for k, v := range c.set {
				attrs[k] = v
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6Factories,
				Steps: []resource.TestStep{{
					Config:      hcl("appsignal_log_trigger", attrs),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(c.err)),
				}},
			})
		})
	}
}
