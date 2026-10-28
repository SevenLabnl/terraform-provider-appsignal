package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

func testClient() *client.Client {
	return client.New(os.Getenv("APPSIGNAL_ENDPOINT"), envToken(), "acceptance-test")
}

func appConfig(name string) string {
	return fmt.Sprintf(`
resource "appsignal_app" "test" {
  organization_slug = %q
  name              = %q
  environment       = "test"
}

data "appsignal_notifier" "email" {
  app_id = appsignal_app.test.id
  name   = "Default email notifier"
}
`, os.Getenv("APPSIGNAL_ORGANIZATION"), name)
}

// saveID stores a resource's id so a later step can compare or act on it.
func saveID(addr string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s not in state", addr)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func idIs(addr string, want *string, same bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		got := s.RootModule().Resources[addr].Primary.ID
		if same && got != *want {
			return fmt.Errorf("%s id changed from %s to %s", addr, *want, got)
		}
		if !same && got == *want {
			return fmt.Errorf("%s id stayed %s", addr, got)
		}
		return nil
	}
}

func appIDOf(s *terraform.State) string {
	return s.RootModule().Resources["appsignal_app.test"].Primary.ID
}

func triggerActive(appID *string, id *string, want bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if *appID == "" {
			*appID = appIDOf(s)
		}
		_, err := testClient().GetTrigger(context.Background(), *appID, *id)
		switch {
		case err == nil && !want:
			return fmt.Errorf("trigger %s is still active", *id)
		case client.IsNotFound(err) && want:
			return fmt.Errorf("trigger %s is not active", *id)
		case err != nil && !client.IsNotFound(err):
			return err
		}
		return nil
	}
}

const triggerFull = `
resource "appsignal_trigger" "test" {
  app_id              = appsignal_app.test.id
  kind                = "Advanced"
  name                = "queue depth"
  description         = "jobs are piling up"
  metric_name         = "queue_depth"
  field               = "gauge"
  tags                = { queue = "default", host = "web*" }
  comparison_operator = ">="
  threshold           = %s
  warmup_duration     = 5
  cooldown_duration   = 2
  no_match_is_zero    = true
  format              = "size"
  format_input        = "kilobyte"
  notifier_ids        = [data.appsignal_notifier.email.id]
}
`

const triggerMinimal = `
resource "appsignal_trigger" "test" {
  app_id              = appsignal_app.test.id
  kind                = "Advanced"
  name                = "queue depth"
  metric_name         = "queue_depth"
  field               = "mean"
  comparison_operator = "<"
  threshold           = 1
}
`

func TestAccTrigger(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_trigger.test"
	var appID, first, second, third string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: appConfig(name) + fmt.Sprintf(triggerFull, "80.5"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "kind", "Advanced"),
					resource.TestCheckResourceAttr(addr, "name", "queue depth"),
					resource.TestCheckResourceAttr(addr, "description", "jobs are piling up"),
					resource.TestCheckResourceAttr(addr, "metric_name", "queue_depth"),
					resource.TestCheckResourceAttr(addr, "field", "gauge"),
					resource.TestCheckResourceAttr(addr, "tags.%", "2"),
					resource.TestCheckResourceAttr(addr, "tags.queue", "default"),
					resource.TestCheckResourceAttr(addr, "tags.host", "web*"),
					resource.TestCheckResourceAttr(addr, "comparison_operator", ">="),
					resource.TestCheckResourceAttr(addr, "threshold", "80.5"),
					resource.TestCheckResourceAttr(addr, "warmup_duration", "5"),
					resource.TestCheckResourceAttr(addr, "cooldown_duration", "2"),
					resource.TestCheckResourceAttr(addr, "no_match_is_zero", "true"),
					resource.TestCheckResourceAttr(addr, "format", "size"),
					resource.TestCheckResourceAttr(addr, "format_input", "kilobyte"),
					resource.TestCheckResourceAttr(addr, "notifier_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "notifier_ids.0", "data.appsignal_notifier.email", "id"),
					saveID(addr, &first),
					triggerActive(&appID, &first, true),
				),
			},
			{
				// Any change makes a new version and archives the old one.
				Config: appConfig(name) + fmt.Sprintf(triggerFull, "90"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "threshold", "90"),
					idIs(addr, &first, false),
					saveID(addr, &second),
					triggerActive(&appID, &first, false),
					triggerActive(&appID, &second, true),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", addr),
			},
			{
				// Removing the optional arguments leaves them unset in the new version.
				Config: appConfig(name) + triggerMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(addr, "description"),
					resource.TestCheckNoResourceAttr(addr, "tags"),
					resource.TestCheckNoResourceAttr(addr, "format"),
					resource.TestCheckNoResourceAttr(addr, "format_input"),
					resource.TestCheckResourceAttr(addr, "notifier_ids.#", "0"),
					resource.TestCheckResourceAttr(addr, "warmup_duration", "0"),
					resource.TestCheckResourceAttr(addr, "cooldown_duration", "0"),
					resource.TestCheckResourceAttr(addr, "no_match_is_zero", "false"),
					resource.TestCheckResourceAttr(addr, "field", "mean"),
					resource.TestCheckResourceAttr(addr, "comparison_operator", "<"),
					saveID(addr, &third),
					triggerActive(&appID, &second, false),
				),
			},
			{
				// A trigger archived outside tofu is created again.
				PreConfig: func() {
					if err := testClient().ArchiveTrigger(context.Background(), appID, third); err != nil {
						t.Fatal(err)
					}
				},
				Config: appConfig(name) + triggerMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &third, false),
					saveID(addr, &first),
					triggerActive(&appID, &first, true),
				),
			},
			{
				// Removing the resource archives the trigger.
				Config: appConfig(name),
				Check:  triggerActive(&appID, &first, false),
			},
		},
	})
}

// Plan-time checks for values the AppSignal API would accept and store as is.
func TestTriggerValidation(t *testing.T) {
	cases := map[string]struct {
		set map[string]string
		err string
	}{
		"format_input without size": {map[string]string{"format": `"number"`, "format_input": `"byte"`}, `format_input needs format = "size"`},
		"unknown format":            {map[string]string{"format": `"bananas"`}, `format value must be one of`},
		"unknown size unit":         {map[string]string{"format": `"size"`, "format_input": `"gigabyte"`}, `format_input value must be one of`},
		"uptime kind":               {map[string]string{"kind": `"UptimeMonitor"`}, `kind value must be one of`},
		"unknown field":             {map[string]string{"field": `"max"`}, `field value must be one of`},
		"negative warmup":           {map[string]string{"warmup_duration": "-1"}, `warmup_duration value must be at least 0`},
		"negative cooldown":         {map[string]string{"cooldown_duration": "-1"}, `cooldown_duration value must be at least 0`},
		"empty name":                {map[string]string{"name": `""`}, `name string length must be at least 1`},
		"empty description":         {map[string]string{"description": `""`}, `description string length must be at least 1`},
		"empty tags":                {map[string]string{"tags": "{}"}, `tags map must contain at least 1`},
		"unknown operator":          {map[string]string{"comparison_operator": `"=>"`}, `comparison_operator value must be one of`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			attrs := map[string]string{
				"app_id": `"x"`, "kind": `"Advanced"`, "name": `"n"`, "metric_name": `"m"`,
				"field": `"mean"`, "comparison_operator": `">"`, "threshold": "1",
			}
			for k, v := range c.set {
				attrs[k] = v
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6Factories,
				Steps: []resource.TestStep{{
					Config:      hcl("appsignal_trigger", attrs),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(c.err)),
				}},
			})
		})
	}
}

// hcl renders one resource with the given raw HCL attribute values.
func hcl(typ string, attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "provider \"appsignal\" {\n  token = \"unused\"\n}\n\nresource %q \"test\" {\n", typ)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s = %s\n", k, attrs[k])
	}
	b.WriteString("}\n")
	return b.String()
}
