package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

// actionOrder lists the app's log actions as "name" in execution order.
func actionOrder(appID string) ([]string, error) {
	all, err := testClient().ListLogActions(context.Background(), appID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for _, a := range all {
		names = append(names, a.Name)
	}
	return names, nil
}

func orderIs(appID *string, want ...string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if *appID == "" {
			*appID = appIDOf(s)
		}
		got, err := actionOrder(*appID)
		if err != nil {
			return err
		}
		if strings.Join(got, " | ") != strings.Join(want, " | ") {
			return fmt.Errorf("log action order is\n  %s\nwant\n  %s", strings.Join(got, " | "), strings.Join(want, " | "))
		}
		return nil
	}
}

const logSource = `
resource "appsignal_log_source" "test" {
  app_id = appsignal_app.test.id
  name   = "acc-source"
  type   = "http_json"
}
`

func TestAccLogFilter(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_log_filter.test"
	var appID, first string
	cfg := func(filterName, query, sources string) string {
		return appConfig(name) + logSource + fmt.Sprintf(`
resource "appsignal_log_filter" "test" {
  app_id     = appsignal_app.test.id
  name       = %q
  query      = %q
  %s
}
`, filterName, query, sources)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: cfg("health checks", `message:"GET /healthz"`, "source_ids = [appsignal_log_source.test.id]"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "health checks"),
					resource.TestCheckResourceAttr(addr, "query", `message:"GET /healthz"`),
					resource.TestCheckResourceAttr(addr, "source_ids.#", "1"),
					saveID(addr, &first),
				),
			},
			{
				Config: cfg("probes", `message:"GET /readyz"`, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckResourceAttr(addr, "name", "probes"),
					resource.TestCheckResourceAttr(addr, "query", `message:"GET /readyz"`),
					resource.TestCheckResourceAttr(addr, "source_ids.#", "0"),
					func(s *terraform.State) error {
						appID = appIDOf(s)
						a, err := testClient().GetLogAction(context.Background(), appID, first)
						if err != nil {
							return err
						}
						if len(a.SourceIDs) != 0 {
							return fmt.Errorf("sources not cleared: %v", a.SourceIDs)
						}
						return nil
					},
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", addr),
			},
			{
				PreConfig: func() {
					if err := testClient().DeleteLogAction(context.Background(), appID, first); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg("probes", `message:"GET /readyz"`, ""),
				Check:  idIs(addr, &first, false),
			},
			{
				Config: appConfig(name) + logSource,
				Check:  orderIs(&appID),
			},
		},
	})
}

const logMetricFull = `
resource "appsignal_log_metric" "test" {
  app_id     = appsignal_app.test.id
  name       = "jobs"
  query      = "message:job"
  drop_lines = true
  metrics = [
    { name = "jobs.done", type = "counter", tags = { queue = "default" } },
    { name = "jobs.duration", type = "distribution", field = "duration_ms" },
    { name = "jobs.backlog", type = "gauge", field = "backlog" },
  ]
}
`

const logMetricSmaller = `
resource "appsignal_log_metric" "test" {
  app_id  = appsignal_app.test.id
  name    = "jobs"
  query   = "message:job"
  metrics = [{ name = "jobs.backlog", type = "counter" }]
}
`

func TestAccLogMetric(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_log_metric.test"
	var appID, first string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: appConfig(name) + logMetricFull,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "drop_lines", "true"),
					resource.TestCheckResourceAttr(addr, "metrics.#", "3"),
					resource.TestCheckResourceAttr(addr, "metrics.0.name", "jobs.done"),
					resource.TestCheckResourceAttr(addr, "metrics.0.type", "counter"),
					resource.TestCheckNoResourceAttr(addr, "metrics.0.field"),
					resource.TestCheckResourceAttr(addr, "metrics.0.tags.queue", "default"),
					resource.TestCheckResourceAttr(addr, "metrics.1.type", "distribution"),
					resource.TestCheckResourceAttr(addr, "metrics.1.field", "duration_ms"),
					resource.TestCheckNoResourceAttr(addr, "metrics.1.tags"),
					resource.TestCheckResourceAttr(addr, "metrics.2.name", "jobs.backlog"),
					resource.TestCheckResourceAttr(addr, "metrics.2.type", "gauge"),
					saveID(addr, &first),
					orderIs(&appID, "jobs", "jobs"+client.DropFilterSuffix),
				),
			},
			{
				ResourceName: addr, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("appsignal_app.test", addr),
			},
			{
				// Dropping off removes AppSignal's filter; the metric list is replaced.
				Config: appConfig(name) + logMetricSmaller,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckResourceAttr(addr, "drop_lines", "false"),
					resource.TestCheckResourceAttr(addr, "metrics.#", "1"),
					resource.TestCheckResourceAttr(addr, "metrics.0.type", "counter"),
					resource.TestCheckNoResourceAttr(addr, "metrics.0.field"),
					orderIs(&appID, "jobs"),
				),
			},
			{
				Config: appConfig(name) + logMetricFull,
				Check:  orderIs(&appID, "jobs", "jobs"+client.DropFilterSuffix),
			},
			{
				// Deleting the action also deletes AppSignal's filter.
				Config: appConfig(name),
				Check:  orderIs(&appID),
			},
		},
	})
}

func logActionsConfig(name, order string) string {
	return appConfig(name) + `
resource "appsignal_log_filter" "f" {
  app_id = appsignal_app.test.id
  name   = "filter"
  query  = "message:healthz"
}

resource "appsignal_log_metric" "m" {
  app_id     = appsignal_app.test.id
  name       = "metric"
  query      = "message:job"
  drop_lines = true
  metrics    = [{ name = "jobs.done", type = "counter" }]
}

resource "appsignal_log_trigger" "t" {
  app_id = appsignal_app.test.id
  name   = "trigger"
  query  = "severity:fatal"
}

resource "appsignal_log_action_order" "test" {
  app_id     = appsignal_app.test.id
  action_ids = [` + order + `]
}
`
}

func TestAccLogActionOrder(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	var appID string
	const (
		f  = "appsignal_log_filter.f.id"
		m  = "appsignal_log_metric.m.id"
		tr = "appsignal_log_trigger.t.id"
	)
	drop := "metric" + client.DropFilterSuffix

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: logActionsConfig(name, tr+", "+m+", "+f),
				Check:  orderIs(&appID, "trigger", "metric", drop, "filter"),
			},
			{
				Config: logActionsConfig(name, f+", "+tr+", "+m),
				Check:  orderIs(&appID, "filter", "trigger", "metric", drop),
			},
			{
				// A reorder outside tofu is put back; the drop filter stays behind its metric.
				PreConfig: func() {
					all, err := testClient().ListLogActions(context.Background(), appID)
					if err != nil {
						t.Fatal(err)
					}
					var ids []string
					for i := len(all) - 1; i >= 0; i-- {
						ids = append(ids, all[i].ID)
					}
					if err := testClient().Do(context.Background(), `mutation($app: String!, $ids: [String]!) {
						reorderLogLineActions(appId: $app, actionIds: $ids) { __typename } }`,
						map[string]any{"app": appID, "ids": ids}, nil); err != nil {
						t.Fatal(err)
					}
					if got, _ := actionOrder(appID); got[0] != drop {
						t.Fatalf("setup did not reverse the order: %v", got)
					}
				},
				Config: logActionsConfig(name, f+", "+tr+", "+m),
				Check:  orderIs(&appID, "filter", "trigger", "metric", drop),
			},
			{
				// An unlisted action moved in front of the listed ones is moved back behind them.
				PreConfig: func() {
					extra, err := testClient().CreateLogAction(context.Background(), appID,
						client.LogAction{Type: client.ActionFilter, Name: "made in the UI", Query: "x"})
					if err != nil {
						t.Fatal(err)
					}
					all, _ := testClient().ListLogActions(context.Background(), appID)
					ids := []string{extra.ID}
					for _, a := range all {
						if a.ID != extra.ID {
							ids = append(ids, a.ID)
						}
					}
					if err := testClient().Do(context.Background(), `mutation($app: String!, $ids: [String]!) {
						reorderLogLineActions(appId: $app, actionIds: $ids) { __typename } }`,
						map[string]any{"app": appID, "ids": ids}, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: logActionsConfig(name, f+", "+tr+", "+m),
				Check:  orderIs(&appID, "filter", "trigger", "metric", drop, "made in the UI"),
			},
			{
				ResourceName: "appsignal_log_action_order.test", ImportState: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) { return appIDOf(s), nil },
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if n := states[0].Attributes["action_ids.#"]; n != "4" {
						return fmt.Errorf("imported order has %s actions, want 4 (drop filter left out)", n)
					}
					return nil
				},
			},
		},
	})
}

func TestLogActionValidation(t *testing.T) {
	base := map[string]string{"app_id": `"a"`, "name": `"n"`, "query": `"q"`}
	cases := []struct {
		name, typ string
		set       map[string]string
		err       string
	}{
		{"metric name with capitals", "appsignal_log_metric", map[string]string{"metrics": `[{ name = "Jobs.Done", type = "counter" }]`}, `must use only a-z, 0-9, dots and underscores`},
		{"metric name with spaces", "appsignal_log_metric", map[string]string{"metrics": `[{ name = "jobs done", type = "counter" }]`}, `must use only a-z, 0-9, dots and underscores`},
		{"duplicate metric", "appsignal_log_metric", map[string]string{"metrics": `[{ name = "a", type = "counter" }, { name = "a", type = "counter" }]`}, `Duplicate metric name`},
		{"gauge without field", "appsignal_log_metric", map[string]string{"metrics": `[{ name = "a", type = "gauge" }]`}, `needs the field that holds its value`},
		{"distribution without field", "appsignal_log_metric", map[string]string{"metrics": `[{ name = "a", type = "distribution" }]`}, `needs the field that holds its value`},
		{"no metrics", "appsignal_log_metric", map[string]string{"metrics": `[]`}, `metrics list must contain at least 1`},
		{"unknown type", "appsignal_log_metric", map[string]string{"metrics": `[{ name = "a", type = "histogram" }]`}, `type value must be one of`},
		{"empty query", "appsignal_log_filter", map[string]string{"query": `""`}, `query string length must be at least 1`},
		{"empty name", "appsignal_log_filter", map[string]string{"name": `""`}, `name string length must be at least 1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			attrs := map[string]string{}
			for k, v := range base {
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
	for name, ids := range map[string]string{"empty order": `[]`, "duplicate id": `["a", "a"]`} {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6Factories,
				Steps: []resource.TestStep{{
					Config:   hcl("appsignal_log_action_order", map[string]string{"app_id": `"a"`, "action_ids": ids}),
					PlanOnly: true, ExpectError: regexp.MustCompile(`action_ids`),
				}},
			})
		})
	}
}
