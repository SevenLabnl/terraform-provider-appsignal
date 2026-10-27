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

const checkInCron = `
resource "appsignal_check_in" "test" {
  app_id            = appsignal_app.test.id
  identifier        = %q
  kind              = "cron"
  schedule          = %q
  timezone          = "Europe/Amsterdam"
  wait_time_minutes = 15
  description       = "nightly export"
  notifier_ids      = [data.appsignal_notifier.email.id]
}
`

const checkInCronMinimal = `
resource "appsignal_check_in" "test" {
  app_id            = appsignal_app.test.id
  identifier        = "nightly-export"
  kind              = "cron"
  schedule          = "30 3 * * 1-5"
  wait_time_minutes = 5
}
`

const checkInHeartbeat = `
resource "appsignal_check_in" "test" {
  app_id            = appsignal_app.test.id
  identifier        = "nightly-export"
  kind              = "heartbeat"
  wait_time_minutes = 5
}
`

func TestAccCheckIn(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_check_in.test"
	var appID, first, second string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: appConfig(name) + fmt.Sprintf(checkInCron, "export", "0 2 * * *"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "identifier", "export"),
					resource.TestCheckResourceAttr(addr, "kind", "cron"),
					resource.TestCheckResourceAttr(addr, "schedule", "0 2 * * *"),
					resource.TestCheckResourceAttr(addr, "timezone", "Europe/Amsterdam"),
					resource.TestCheckResourceAttr(addr, "wait_time_minutes", "15"),
					resource.TestCheckResourceAttr(addr, "description", "nightly export"),
					resource.TestCheckResourceAttr(addr, "notifier_ids.#", "1"),
					saveID(addr, &first),
				),
			},
			{
				Config: appConfig(name) + fmt.Sprintf(checkInCron, "nightly-export", "0 4 * * *"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "identifier", "nightly-export"),
					resource.TestCheckResourceAttr(addr, "schedule", "0 4 * * *"),
					idIs(addr, &first, true),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", addr),
			},
			{
				// The timezone goes back to UTC and the description and notifiers are cleared.
				Config: appConfig(name) + checkInCronMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckResourceAttr(addr, "timezone", "UTC"),
					resource.TestCheckResourceAttr(addr, "wait_time_minutes", "5"),
					resource.TestCheckNoResourceAttr(addr, "description"),
					resource.TestCheckResourceAttr(addr, "notifier_ids.#", "0"),
					func(s *terraform.State) error {
						appID = appIDOf(s)
						ci, err := testClient().GetCheckIn(context.Background(), appID, first)
						if err != nil {
							return err
						}
						if ci.Timezone != "UTC" || (ci.Description != nil && *ci.Description != "") || len(ci.Notifiers) != 0 {
							return fmt.Errorf("AppSignal kept old values: %+v", ci)
						}
						return nil
					},
				),
			},
			{
				// A different kind is a new check-in.
				Config: appConfig(name) + checkInHeartbeat,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "kind", "heartbeat"),
					resource.TestCheckNoResourceAttr(addr, "schedule"),
					resource.TestCheckResourceAttr(addr, "timezone", "UTC"),
					idIs(addr, &first, false),
					saveID(addr, &second),
					func(s *terraform.State) error {
						if _, err := testClient().GetCheckIn(context.Background(), appID, first); !client.IsNotFound(err) {
							return fmt.Errorf("old check-in %s still exists (err %v)", first, err)
						}
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					if err := testClient().DeleteCheckIn(context.Background(), appID, second); err != nil {
						t.Fatal(err)
					}
				},
				Config: appConfig(name) + checkInHeartbeat,
				Check:  idIs(addr, &second, false),
			},
			{
				// A second check-in with the same identifier is refused.
				Config: appConfig(name) + checkInHeartbeat + `
resource "appsignal_check_in" "dup" {
  app_id            = appsignal_app.test.id
  identifier        = "nightly-export"
  kind              = "heartbeat"
  wait_time_minutes = 5
  depends_on        = [appsignal_check_in.test]
}
`,
				ExpectError: regexp.MustCompile(`Check-in identifier in use`),
			},
			{
				Config: appConfig(name),
				Check: func(s *terraform.State) error {
					all, err := testClient().ListCheckIns(context.Background(), appID)
					if err != nil {
						return err
					}
					if len(all) != 0 {
						return fmt.Errorf("check-ins left: %+v", all)
					}
					return nil
				},
			},
		},
	})
}

func TestCheckInValidation(t *testing.T) {
	cases := map[string]struct {
		set map[string]string
		err string
	}{
		"cron without schedule": {map[string]string{"kind": `"cron"`}, `A cron check-in needs a schedule`},
		"six-field schedule":    {map[string]string{"kind": `"cron"`, "schedule": `"0 0 2 * * *"`}, `is not a five-field cron expression`},
		"words as schedule":     {map[string]string{"kind": `"cron"`, "schedule": `"every day at 2"`}, `is not a five-field cron expression`},
		"bad characters":        {map[string]string{"kind": `"cron"`, "schedule": `"0 2 * * ?"`}, `is not a five-field cron expression`},
		"unknown timezone":      {map[string]string{"kind": `"cron"`, "schedule": `"0 2 * * *"`, "timezone": `"Mars/Olympus"`}, `Unknown timezone`},
		"local timezone":        {map[string]string{"kind": `"cron"`, "schedule": `"0 2 * * *"`, "timezone": `"Local"`}, `Unknown timezone`},
		"heartbeat schedule":    {map[string]string{"schedule": `"0 2 * * *"`}, `Only cron check-ins have a schedule`},
		"heartbeat timezone":    {map[string]string{"timezone": `"Europe/Amsterdam"`}, `Only cron check-ins have a timezone`},
		"zero wait":             {map[string]string{"wait_time_minutes": "0"}, `wait_time_minutes value must be at least 1`},
		"identifier with space": {map[string]string{"identifier": `"nightly export"`}, `must be one word without spaces`},
		"unknown kind":          {map[string]string{"kind": `"interval"`}, `kind value must be one of`},
		"empty description":     {map[string]string{"description": `""`}, `description string length must be at least 1`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			attrs := map[string]string{"app_id": `"a"`, "identifier": `"job"`, "kind": `"heartbeat"`, "wait_time_minutes": "5"}
			for k, v := range c.set {
				attrs[k] = v
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6Factories,
				Steps: []resource.TestStep{{
					Config: hcl("appsignal_check_in", attrs), PlanOnly: true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(c.err)),
				}},
			})
		})
	}
	// A valid cron check-in passes validation.
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: hcl("appsignal_check_in", map[string]string{
				"app_id": `"a"`, "identifier": `"job"`, "kind": `"cron"`, "schedule": `"*/15 9-17 * JAN-MAR MON,FRI"`,
				"timezone": `"America/New_York"`, "wait_time_minutes": "5",
			}),
			PlanOnly: true, ExpectNonEmptyPlan: true,
		}},
	})
}
