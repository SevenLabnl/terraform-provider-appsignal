package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccUptimeMonitorDataSource(t *testing.T) {
	org := os.Getenv("APPSIGNAL_ORGANIZATION")
	base := fmt.Sprintf(`
resource "appsignal_app" "test" {
  organization_slug = %q
  name              = %q
  environment       = "test"
}

data "appsignal_notifier" "email" {
  app_id = appsignal_app.test.id
  name   = "Default email notifier"
}

resource "appsignal_uptime_monitor" "full" {
  app_id                 = appsignal_app.test.id
  name                   = "full"
  url                    = "https://example.com/"
  regions                = ["europe", "asia_pacific"]
  warmup_duration        = 3
  description            = "acceptance test"
  notifier_ids           = [data.appsignal_notifier.email.id]
  headers                = { "X-Test" = "1" }
  check_body_content     = "Example Domain"
  monitor_ssl_expiration = true
}

resource "appsignal_uptime_monitor" "bare" {
  app_id  = appsignal_app.test.id
  name    = "bare"
  url     = "https://example.org/"
  regions = ["europe"]
}

resource "appsignal_uptime_monitor" "twin" {
  app_id  = appsignal_app.test.id
  name    = "twin"
  url     = "https://example.org/"
  regions = ["europe"]
}
`, org, acctest.RandomWithPrefix("tf-acc"))

	lookups := `
data "appsignal_uptime_monitor" "by_id" {
  app_id = appsignal_app.test.id
  id     = appsignal_uptime_monitor.full.id
}

data "appsignal_uptime_monitor" "by_name" {
  app_id     = appsignal_app.test.id
  name       = "full"
  depends_on = [appsignal_uptime_monitor.full]
}

data "appsignal_uptime_monitor" "by_url" {
  app_id     = appsignal_app.test.id
  url        = "https://example.com/"
  depends_on = [appsignal_uptime_monitor.full]
}

data "appsignal_uptime_monitor" "bare" {
  app_id     = appsignal_app.test.id
  name       = "bare"
  depends_on = [appsignal_uptime_monitor.bare]
}
`
	lookup := func(attr, value string) string {
		return base + fmt.Sprintf(`
data "appsignal_uptime_monitor" "x" {
  app_id     = appsignal_app.test.id
  %s = %q
  depends_on = [appsignal_uptime_monitor.full, appsignal_uptime_monitor.bare, appsignal_uptime_monitor.twin]
}
`, attr, value)
	}

	full := "appsignal_uptime_monitor.full"
	var checks []resource.TestCheckFunc
	for _, ds := range []string{"data.appsignal_uptime_monitor.by_id", "data.appsignal_uptime_monitor.by_name", "data.appsignal_uptime_monitor.by_url"} {
		for _, a := range []string{"id", "name", "url", "description", "warmup_duration", "check_body_content",
			"monitor_ssl_expiration", "headers.%", "headers.X-Test", "regions.#", "notifier_ids.#"} {
			checks = append(checks, resource.TestCheckResourceAttrPair(ds, a, full, a))
		}
		checks = append(checks,
			resource.TestCheckTypeSetElemAttr(ds, "regions.*", "asia_pacific"),
			resource.TestCheckTypeSetElemAttrPair(ds, "notifier_ids.*", "data.appsignal_notifier.email", "id"))
	}
	bare := "data.appsignal_uptime_monitor.bare"
	checks = append(checks,
		resource.TestCheckResourceAttrPair(bare, "id", "appsignal_uptime_monitor.bare", "id"),
		resource.TestCheckNoResourceAttr(bare, "description"),
		resource.TestCheckNoResourceAttr(bare, "check_body_content"),
		resource.TestCheckNoResourceAttr(bare, "headers.%"),
		resource.TestCheckResourceAttr(bare, "monitor_ssl_expiration", "false"),
		resource.TestCheckResourceAttr(bare, "notifier_ids.#", "0"),
		resource.TestCheckResourceAttr(bare, "regions.#", "1"),
	)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: base + lookups, Check: resource.ComposeAggregateTestCheckFunc(checks...)},
			{Config: lookup("url", "https://example.org/"), ExpectError: regexp.MustCompile(`2 uptime monitors have url`)},
			{Config: lookup("name", "missing"), ExpectError: regexp.MustCompile(`no uptime monitor with name "missing"`)},
			{Config: lookup("id", "000000000000000000000000"), ExpectError: regexp.MustCompile(`Reading AppSignal uptime monitor`)},
			// End on a valid config: the post-test destroy runs with the last one.
			{Config: base + lookups},
		},
	})
}

func TestAccUptimeMonitorDataSourceLookupKeys(t *testing.T) {
	cfg := func(keys string) string {
		return fmt.Sprintf(`
data "appsignal_uptime_monitor" "x" {
  app_id = "000000000000000000000000"
  %s
}
`, keys)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: cfg("name = \"a\"\n  url = \"https://example.com/\""), PlanOnly: true,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`)},
			{Config: cfg(""), PlanOnly: true, ExpectError: regexp.MustCompile(`Missing Attribute Configuration`)},
		},
	})
}
