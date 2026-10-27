package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var protoV6Factories = map[string]func() (tfprotov6.ProviderServer, error){
	"appsignal": providerserver.NewProtocol6WithError(New("test")()),
}

// Acceptance tests create a real app in APPSIGNAL_ORGANIZATION and delete it again.
func preCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("APPSIGNAL_TOKEN") == "" || os.Getenv("APPSIGNAL_ORGANIZATION") == "" {
		t.Fatal("APPSIGNAL_TOKEN and APPSIGNAL_ORGANIZATION must be set for acceptance tests")
	}
}

func config(org, name string, warmup int, extra string) string {
	return fmt.Sprintf(`
resource "appsignal_app" "test" {
  organization_slug = %[1]q
  name              = %[2]q
  environment       = "test"
}

data "appsignal_notifier" "email" {
  app_id = appsignal_app.test.id
  name   = "Default email notifier"
}

resource "appsignal_log_source" "test" {
  app_id         = appsignal_app.test.id
  name           = "acc-source"
  type           = "http_json"
  format         = "json"
  parse_severity = true
}

resource "appsignal_uptime_monitor" "test" {
  app_id          = appsignal_app.test.id
  name            = "example.com"
  url             = "https://example.com/"
  regions         = ["europe", "north_america"]
  warmup_duration = %[3]d
  %[4]s
}

data "appsignal_app" "by_name" {
  organization_slug = %[1]q
  name              = appsignal_app.test.name
  environment       = appsignal_app.test.environment
}
`, org, name, warmup, extra)
}

func importID(app, res string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		return s.RootModule().Resources[app].Primary.ID + "/" + s.RootModule().Resources[res].Primary.ID, nil
	}
}

func TestAccAppLogSourceAndUptimeMonitor(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	full := `description            = "acceptance test"
  notifier_ids           = [data.appsignal_notifier.email.id]
  headers                = { "X-Test" = "1" }
  check_body_content     = "Example Domain"
  monitor_ssl_expiration = true`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: config(os.Getenv("APPSIGNAL_ORGANIZATION"), name, 2, full),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("appsignal_app.test", "push_api_key"),
					resource.TestCheckResourceAttrPair("data.appsignal_app.by_name", "id", "appsignal_app.test", "id"),
					resource.TestCheckResourceAttr("appsignal_log_source.test", "format", "json"),
					resource.TestCheckResourceAttrSet("appsignal_log_source.test", "key"),
					resource.TestCheckResourceAttr("appsignal_uptime_monitor.test", "warmup_duration", "2"),
					resource.TestCheckResourceAttr("appsignal_uptime_monitor.test", "notifier_ids.#", "1"),
					resource.TestCheckResourceAttr("appsignal_uptime_monitor.test", "headers.X-Test", "1"),
				),
			},
			{
				// An in-place update keeps the id.
				Config: config(os.Getenv("APPSIGNAL_ORGANIZATION"), name, 5, full),
				Check:  resource.TestCheckResourceAttr("appsignal_uptime_monitor.test", "warmup_duration", "5"),
			},
			{ResourceName: "appsignal_app.test", ImportState: true, ImportStateVerify: true},
			{
				ResourceName: "appsignal_log_source.test", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", "appsignal_log_source.test"),
			},
			{
				ResourceName: "appsignal_uptime_monitor.test", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", "appsignal_uptime_monitor.test"),
			},
			{
				// Removing the optional arguments clears them in AppSignal, not only in state.
				Config: config(os.Getenv("APPSIGNAL_ORGANIZATION"), name, 5, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("appsignal_uptime_monitor.test", "description"),
					resource.TestCheckNoResourceAttr("appsignal_uptime_monitor.test", "check_body_content"),
					resource.TestCheckResourceAttr("appsignal_uptime_monitor.test", "notifier_ids.#", "0"),
				),
			},
		},
	})
}

func appIsGone(s *terraform.State) error {
	c := client.New(os.Getenv("APPSIGNAL_ENDPOINT"), os.Getenv("APPSIGNAL_TOKEN"), "acceptance-test")
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "appsignal_app" {
			continue
		}
		_, err := c.GetApp(context.Background(), rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("app %s still exists", rs.Primary.ID)
		}
		if !client.IsNotFound(err) {
			return err
		}
	}
	return nil
}
