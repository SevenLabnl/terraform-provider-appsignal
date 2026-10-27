package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccStatusPage needs APPSIGNAL_STATUS_PAGE_HOSTNAME: a hostname with a CNAME to
// cname.appsignal-status.com, which AppSignal checks when the page is created.
func TestAccStatusPage(t *testing.T) {
	hostname := os.Getenv("APPSIGNAL_STATUS_PAGE_HOSTNAME")
	if hostname == "" {
		t.Skip("APPSIGNAL_STATUS_PAGE_HOSTNAME is not set")
	}
	name := acctest.RandomWithPrefix("tf-acc")
	org := os.Getenv("APPSIGNAL_ORGANIZATION")
	const addr = "appsignal_status_page.test"
	var first string
	cfg := func(title, extra string) string {
		return appConfig(name) + fmt.Sprintf(`
resource "appsignal_uptime_monitor" "a" {
  app_id  = appsignal_app.test.id
  name    = "a"
  url     = "https://example.com/"
  regions = ["europe"]
}

resource "appsignal_uptime_monitor" "b" {
  app_id  = appsignal_app.test.id
  name    = "b"
  url     = "https://example.org/"
  regions = ["europe"]
}

resource "appsignal_status_page" "test" {
  organization_slug = %q
  hostname          = %q
  title             = %q
  %s
}
`, org, hostname, title, extra)
	}
	full := `description        = "Service status"
  threshold          = 2
  uptime_monitor_ids = [appsignal_uptime_monitor.a.id, appsignal_uptime_monitor.b.id]`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy: func(s *terraform.State) error {
			if first == "" {
				return nil
			}
			if _, err := testClient().GetStatusPage(context.Background(), org, first); err == nil {
				return fmt.Errorf("status page %s still exists", first)
			}
			return appIsGone(s)
		},
		Steps: []resource.TestStep{
			{
				Config: cfg("Status", full),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "hostname", hostname),
					resource.TestCheckResourceAttr(addr, "title", "Status"),
					resource.TestCheckResourceAttr(addr, "description", "Service status"),
					resource.TestCheckResourceAttr(addr, "threshold", "2"),
					resource.TestCheckResourceAttr(addr, "uptime_monitor_ids.#", "2"),
					saveID(addr, &first),
				),
			},
			{
				Config: cfg("Our status", full),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", "Our status"),
					idIs(addr, &first, true),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) { return org + "/" + first, nil },
			},
			{
				Config: cfg("Our status", "threshold = 1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckNoResourceAttr(addr, "description"),
					resource.TestCheckResourceAttr(addr, "threshold", "1"),
					resource.TestCheckResourceAttr(addr, "uptime_monitor_ids.#", "0"),
					func(s *terraform.State) error {
						p, err := testClient().GetStatusPage(context.Background(), org, first)
						if err != nil {
							return err
						}
						if (p.Description != nil && *p.Description != "") || len(p.UptimeMonitorIDs) != 0 {
							return fmt.Errorf("AppSignal kept old values: %+v", p)
						}
						return nil
					},
				),
			},
			{
				// AppSignal drops unknown monitor ids silently; the provider reports them.
				Config:      cfg("Our status", "threshold = 1\n  uptime_monitor_ids = [\"000000000000000000000000\"]"),
				ExpectError: regexp.MustCompile(`Unknown uptime monitors`),
			},
			{
				PreConfig: func() {
					if err := testClient().DeleteStatusPage(context.Background(), org, first); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg("Our status", "threshold = 1"),
				Check:  resource.ComposeAggregateTestCheckFunc(idIs(addr, &first, false), saveID(addr, &first)),
			},
		},
	})
}

func TestStatusPageValidation(t *testing.T) {
	cases := map[string]struct {
		set map[string]string
		err string
	}{
		"url as hostname":  {map[string]string{"hostname": `"https://status.example.com"`}, `must be a hostname`},
		"path in hostname": {map[string]string{"hostname": `"status.example.com/x"`}, `must be a hostname`},
		"single label":     {map[string]string{"hostname": `"status"`}, `must be a hostname`},
		"zero threshold":   {map[string]string{"threshold": "0"}, `threshold value must be at least 1`},
		"empty title":      {map[string]string{"title": `""`}, `title string length must be at least 1`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			attrs := map[string]string{"organization_slug": `"o"`, "hostname": `"status.example.com"`, "title": `"t"`, "threshold": "1"}
			for k, v := range c.set {
				attrs[k] = v
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6Factories,
				Steps: []resource.TestStep{{
					Config: hcl("appsignal_status_page", attrs), PlanOnly: true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(c.err)),
				}},
			})
		})
	}
}
