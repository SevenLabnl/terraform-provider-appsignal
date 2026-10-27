package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func importVisualID(addr string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs := s.RootModule().Resources[addr].Primary
		return rs.Attributes["app_id"] + "/" + rs.Attributes["dashboard_id"] + "/" + rs.ID, nil
	}
}

func dashboardConfig(name, title, description string) string {
	desc := ""
	if description != "" {
		desc = fmt.Sprintf("description = %q", description)
	}
	return appConfig(name) + fmt.Sprintf(`
resource "appsignal_dashboard" "test" {
  app_id = appsignal_app.test.id
  title  = %q
  %s
}
`, title, desc)
}

func TestAccDashboard(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_dashboard.test"
	var appID, first string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(name, "Workers", "Queue and job health"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", "Workers"),
					resource.TestCheckResourceAttr(addr, "description", "Queue and job health"),
					saveID(addr, &first),
				),
			},
			{
				Config: dashboardConfig(name, "Background workers", "Queue and job health"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", "Background workers"),
					idIs(addr, &first, true),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", addr),
			},
			{
				Config: dashboardConfig(name, "Background workers", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(addr, "description"),
					func(s *terraform.State) error {
						appID = appIDOf(s)
						d, err := testClient().GetDashboard(context.Background(), appID, first)
						if err != nil {
							return err
						}
						if d.Description != nil && *d.Description != "" {
							return fmt.Errorf("description still %q", *d.Description)
						}
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					if err := testClient().DeleteDashboard(context.Background(), appID, first); err != nil {
						t.Fatal(err)
					}
				},
				Config: dashboardConfig(name, "Background workers", ""),
				Check:  idIs(addr, &first, false),
			},
		},
	})
}

const timeseriesFull = `
resource "appsignal_dashboard_timeseries" "test" {
  app_id            = appsignal_app.test.id
  dashboard_id      = appsignal_dashboard.test.id
  title             = "Memory"
  description       = "Resident memory per host"
  format            = "size"
  format_input      = "kilobyte"
  display           = %q
  draw_null_as_zero = false
  line_label        = "%%name%% %%field%% on %%hostname%%"
  min_y_axis        = 0.5
  layout            = { x = 0, y = 2, w = 6, h = 4 }
  metrics = [
    { name = "memory_rss", fields = ["p95", "mean"], tags = { hostname = "*" } },
    { name = "memory_swap", fields = ["gauge"] },
  ]
}
`

const timeseriesMinimal = `
resource "appsignal_dashboard_timeseries" "test" {
  app_id       = appsignal_app.test.id
  dashboard_id = appsignal_dashboard.test.id
  title        = "Memory"
  metrics      = [{ name = "memory_rss", fields = ["mean"] }]
}
`

func TestAccDashboardTimeseries(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_dashboard_timeseries.test"
	var appID, dashboardID, first string
	base := dashboardConfig(name, "Hosts", "")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: base + fmt.Sprintf(timeseriesFull, "area"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", "Memory"),
					resource.TestCheckResourceAttr(addr, "description", "Resident memory per host"),
					resource.TestCheckResourceAttr(addr, "format", "size"),
					resource.TestCheckResourceAttr(addr, "format_input", "kilobyte"),
					resource.TestCheckResourceAttr(addr, "display", "area"),
					resource.TestCheckResourceAttr(addr, "draw_null_as_zero", "false"),
					resource.TestCheckResourceAttr(addr, "line_label", "%name% %field% on %hostname%"),
					resource.TestCheckResourceAttr(addr, "min_y_axis", "0.5"),
					resource.TestCheckResourceAttr(addr, "layout.x", "0"),
					resource.TestCheckResourceAttr(addr, "layout.y", "2"),
					resource.TestCheckResourceAttr(addr, "layout.w", "6"),
					resource.TestCheckResourceAttr(addr, "layout.h", "4"),
					resource.TestCheckResourceAttr(addr, "metrics.#", "2"),
					resource.TestCheckResourceAttr(addr, "metrics.0.name", "memory_rss"),
					resource.TestCheckResourceAttr(addr, "metrics.0.fields.0", "p95"),
					resource.TestCheckResourceAttr(addr, "metrics.0.fields.1", "mean"),
					resource.TestCheckResourceAttr(addr, "metrics.0.tags.hostname", "*"),
					resource.TestCheckResourceAttr(addr, "metrics.1.name", "memory_swap"),
					resource.TestCheckNoResourceAttr(addr, "metrics.1.tags"),
					saveID(addr, &first),
				),
			},
			{
				Config: base + fmt.Sprintf(timeseriesFull, "area_relative"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "display", "area_relative"),
					idIs(addr, &first, true),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importVisualID(addr),
			},
			{
				// Optional arguments are cleared; the layout, left out, stays where it was.
				Config: base + timeseriesMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckNoResourceAttr(addr, "description"),
					resource.TestCheckNoResourceAttr(addr, "format_input"),
					resource.TestCheckNoResourceAttr(addr, "line_label"),
					resource.TestCheckNoResourceAttr(addr, "min_y_axis"),
					resource.TestCheckResourceAttr(addr, "format", "number"),
					resource.TestCheckResourceAttr(addr, "display", "line"),
					resource.TestCheckResourceAttr(addr, "draw_null_as_zero", "true"),
					resource.TestCheckResourceAttr(addr, "metrics.#", "1"),
					resource.TestCheckNoResourceAttr(addr, "metrics.0.tags"),
					resource.TestCheckResourceAttr(addr, "layout.y", "2"),
					func(s *terraform.State) error {
						appID = appIDOf(s)
						dashboardID = s.RootModule().Resources["appsignal_dashboard.test"].Primary.ID
						v, err := testClient().GetVisual(context.Background(), appID, dashboardID, first)
						if err != nil {
							return err
						}
						if (v.Description != nil && *v.Description != "") || v.FormatInput != nil || v.LineLabel != nil ||
							v.MinYAxis != nil || len(v.Metrics) != 1 || len(v.Metrics[0].Tags) != 0 {
							return fmt.Errorf("AppSignal kept old values: %+v", v)
						}
						if v.Layout == nil || v.Layout.Y != 2 || v.Layout.W != 6 {
							return fmt.Errorf("layout was not kept: %+v", v.Layout)
						}
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					if err := testClient().DeleteVisual(context.Background(), appID, first); err != nil {
						t.Fatal(err)
					}
				},
				Config: base + timeseriesMinimal,
				Check:  idIs(addr, &first, false),
			},
			{
				// Deleting the dashboard outside tofu takes the visual with it; both come back.
				PreConfig: func() {
					if err := testClient().DeleteDashboard(context.Background(), appID, dashboardID); err != nil {
						t.Fatal(err)
					}
				},
				Config: base + timeseriesMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(addr, "dashboard_id", "appsignal_dashboard.test", "id"),
					func(s *terraform.State) error {
						if s.RootModule().Resources["appsignal_dashboard.test"].Primary.ID == dashboardID {
							return fmt.Errorf("dashboard was not recreated")
						}
						return nil
					},
				),
			},
			{
				Config: base,
				Check: func(s *terraform.State) error {
					d := s.RootModule().Resources["appsignal_dashboard.test"].Primary.ID
					ids, err := testClient().GetDashboardVisualIDs(context.Background(), appIDOf(s), d)
					if err != nil {
						return err
					}
					if len(ids) != 0 {
						return fmt.Errorf("visuals left on the dashboard: %v", ids)
					}
					return nil
				},
			},
		},
	})
}

const numberFull = `
resource "appsignal_dashboard_number" "test" {
  app_id       = appsignal_app.test.id
  dashboard_id = appsignal_dashboard.test.id
  title        = %q
  description  = "Mean response time"
  format       = "duration"
  layout       = { x = 6, y = 0, w = 3, h = 2 }
  metric = {
    name      = "transaction_duration"
    field     = "mean"
    aggregate = "avg"
    tags      = { namespace = "web" }
  }
}
`

const numberMinimal = `
resource "appsignal_dashboard_number" "test" {
  app_id       = appsignal_app.test.id
  dashboard_id = appsignal_dashboard.test.id
  title        = "Errors"
  metric       = { name = "transaction_exception_count", field = "counter", aggregate = "sum" }
}
`

func TestAccDashboardNumber(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_dashboard_number.test"
	var appID, first string
	base := dashboardConfig(name, "Web", "")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: base + fmt.Sprintf(numberFull, "Response time"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", "Response time"),
					resource.TestCheckResourceAttr(addr, "description", "Mean response time"),
					resource.TestCheckResourceAttr(addr, "format", "duration"),
					resource.TestCheckResourceAttr(addr, "layout.x", "6"),
					resource.TestCheckResourceAttr(addr, "layout.w", "3"),
					resource.TestCheckResourceAttr(addr, "metric.name", "transaction_duration"),
					resource.TestCheckResourceAttr(addr, "metric.field", "mean"),
					resource.TestCheckResourceAttr(addr, "metric.aggregate", "avg"),
					resource.TestCheckResourceAttr(addr, "metric.tags.namespace", "web"),
					saveID(addr, &first),
				),
			},
			{
				Config: base + fmt.Sprintf(numberFull, "Web response time"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", "Web response time"),
					idIs(addr, &first, true),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importVisualID(addr),
			},
			{
				// The id of a number tile does not import as a timeseries graph.
				// An import step with its own config gets no provider mapping from the framework.
				Config: `terraform {
  required_providers {
    appsignal = { source = "sevenlabnl/appsignal" }
  }
}
` + base + fmt.Sprintf(numberFull, "Web response time") + timeseriesMinimal,
				ResourceName:      "appsignal_dashboard_timeseries.test",
				ImportState:       true,
				ImportStateIdFunc: importVisualID(addr),
				ExpectError:       regexp.MustCompile(`Wrong kind of visual`),
			},
			{
				Config: base + numberMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckNoResourceAttr(addr, "description"),
					resource.TestCheckResourceAttr(addr, "format", "number"),
					resource.TestCheckResourceAttr(addr, "metric.name", "transaction_exception_count"),
					resource.TestCheckResourceAttr(addr, "metric.field", "counter"),
					resource.TestCheckResourceAttr(addr, "metric.aggregate", "sum"),
					resource.TestCheckNoResourceAttr(addr, "metric.tags"),
					resource.TestCheckResourceAttr(addr, "layout.x", "6"),
					func(s *terraform.State) error {
						appID = appIDOf(s)
						d := s.RootModule().Resources["appsignal_dashboard.test"].Primary.ID
						v, err := testClient().GetVisual(context.Background(), appID, d, first)
						if err != nil {
							return err
						}
						if (v.Description != nil && *v.Description != "") || v.Metric == nil || len(v.Metric.Tags) != 0 {
							return fmt.Errorf("AppSignal kept old values: %+v %+v", v, v.Metric)
						}
						return nil
					},
				),
			},
		},
	})
}

// Visuals created in parallel on one dashboard each get their own id.
func TestAccDashboardParallelVisuals(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	cfg := dashboardConfig(name, "Parallel", "") + `
resource "appsignal_dashboard_number" "n" {
  count        = 5
  app_id       = appsignal_app.test.id
  dashboard_id = appsignal_dashboard.test.id
  title        = "tile ${count.index}"
  metric       = { name = "m${count.index}", field = "gauge", aggregate = "last" }
}

resource "appsignal_dashboard_timeseries" "t" {
  count        = 5
  app_id       = appsignal_app.test.id
  dashboard_id = appsignal_dashboard.test.id
  title        = "graph ${count.index}"
  metrics      = [{ name = "m${count.index}", fields = ["mean"] }]
}

resource "appsignal_trigger" "linked" {
  app_id              = appsignal_app.test.id
  kind                = "Advanced"
  name                = "linked"
  metric_name         = "m0"
  field               = "gauge"
  comparison_operator = ">"
  threshold           = 1
  dashboard_id        = appsignal_dashboard.test.id
}
`
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttrPair("appsignal_trigger.linked", "dashboard_id", "appsignal_dashboard.test", "id"),
		func(s *terraform.State) error {
			seen := map[string]string{}
			for addr, rs := range s.RootModule().Resources {
				if rs.Type != "appsignal_dashboard_number" && rs.Type != "appsignal_dashboard_timeseries" {
					continue
				}
				if other, dup := seen[rs.Primary.ID]; dup {
					return fmt.Errorf("%s and %s share id %s", addr, other, rs.Primary.ID)
				}
				seen[rs.Primary.ID] = addr
			}
			if len(seen) != 10 {
				return fmt.Errorf("want 10 visuals, got %d", len(seen))
			}
			return nil
		},
	}
	for i := 0; i < 5; i++ {
		checks = append(checks,
			resource.TestCheckResourceAttr(fmt.Sprintf("appsignal_dashboard_number.n.%d", i), "title", fmt.Sprintf("tile %d", i)),
			resource.TestCheckResourceAttr(fmt.Sprintf("appsignal_dashboard_number.n.%d", i), "metric.name", fmt.Sprintf("m%d", i)),
			resource.TestCheckResourceAttr(fmt.Sprintf("appsignal_dashboard_timeseries.t.%d", i), "title", fmt.Sprintf("graph %d", i)),
			resource.TestCheckResourceAttr(fmt.Sprintf("appsignal_dashboard_timeseries.t.%d", i), "metrics.0.name", fmt.Sprintf("m%d", i)),
		)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps:                    []resource.TestStep{{Config: cfg, Check: resource.ComposeAggregateTestCheckFunc(checks...)}},
	})
}

func TestVisualValidation(t *testing.T) {
	ts := map[string]string{
		"app_id": `"a"`, "dashboard_id": `"d"`, "title": `"t"`,
		"metrics": `[{ name = "m", fields = ["mean"] }]`,
	}
	nb := map[string]string{
		"app_id": `"a"`, "dashboard_id": `"d"`, "title": `"t"`,
		"metric": `{ name = "m", field = "mean", aggregate = "avg" }`,
	}
	cases := []struct {
		name, typ string
		base, set map[string]string
		err       string
	}{
		{"no metrics", "appsignal_dashboard_timeseries", ts, map[string]string{"metrics": "[]"}, `metrics list must contain at least 1`},
		{"no fields", "appsignal_dashboard_timeseries", ts, map[string]string{"metrics": `[{ name = "m", fields = [] }]`}, `fields list must contain at least 1`},
		{"duplicate field", "appsignal_dashboard_timeseries", ts, map[string]string{"metrics": `[{ name = "m", fields = ["mean", "mean"] }]`}, `Duplicate List Value`},
		{"unknown field", "appsignal_dashboard_timeseries", ts, map[string]string{"metrics": `[{ name = "m", fields = ["max"] }]`}, `value must be one of`},
		{"empty tags", "appsignal_dashboard_timeseries", ts, map[string]string{"metrics": `[{ name = "m", fields = ["mean"], tags = {} }]`}, `tags map must contain at least 1`},
		{"unknown display", "appsignal_dashboard_timeseries", ts, map[string]string{"display": `"bar"`}, `display value must be one of`},
		{"negative x", "appsignal_dashboard_timeseries", ts, map[string]string{"layout": `{ x = -1, y = 0, w = 1, h = 1 }`}, `x value must be at least 0`},
		{"zero width", "appsignal_dashboard_timeseries", ts, map[string]string{"layout": `{ x = 0, y = 0, w = 0, h = 1 }`}, `w value must be at least 1`},
		{"format_input without size", "appsignal_dashboard_timeseries", ts, map[string]string{"format_input": `"byte"`}, `format_input needs format = "size"`},
		{"empty line label", "appsignal_dashboard_timeseries", ts, map[string]string{"line_label": `""`}, `line_label string length must be at least 1`},
		{"unknown aggregate", "appsignal_dashboard_number", nb, map[string]string{"metric": `{ name = "m", field = "mean", aggregate = "median" }`}, `aggregate value must be one of`},
		{"unknown format", "appsignal_dashboard_number", nb, map[string]string{"format": `"bytes"`}, `format value must be one of`},
		{"empty title", "appsignal_dashboard_number", nb, map[string]string{"title": `""`}, `title string length must be at least 1`},
		{"empty dashboard title", "appsignal_dashboard", map[string]string{"app_id": `"a"`}, map[string]string{"title": `""`}, `title string length must be at least 1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			attrs := map[string]string{}
			for k, v := range c.base {
				attrs[k] = v
			}
			for k, v := range c.set {
				attrs[k] = v
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6Factories,
				Steps: []resource.TestStep{{
					Config: hcl(c.typ, attrs), PlanOnly: true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(c.err)),
				}},
			})
		})
	}
}
