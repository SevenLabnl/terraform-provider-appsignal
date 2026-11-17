package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

func logViewExists(appID *string, id *string, want bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if *appID == "" {
			*appID = appIDOf(s)
		}
		_, err := testClient().GetLogView(context.Background(), *appID, *id)
		switch {
		case err == nil && !want:
			return fmt.Errorf("log view %s still exists", *id)
		case client.IsNotFound(err) && want:
			return fmt.Errorf("log view %s does not exist", *id)
		case err != nil && !client.IsNotFound(err):
			return err
		}
		return nil
	}
}

const logViewFull = `
resource "appsignal_log_source" "test" {
  app_id = appsignal_app.test.id
  name   = "acc-source"
  type   = "http_json"
}

resource "appsignal_log_view" "test" {
  app_id      = appsignal_app.test.id
  name        = %q
  source_ids  = [appsignal_log_source.test.id]
  query       = "severity:error"
  columns     = ["hostname", "group"]
  line_height = "1"
}
`

const logViewMinimal = `
resource "appsignal_log_view" "test" {
  app_id = appsignal_app.test.id
  name   = "everything"
}
`

func TestAccLogView(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	const addr = "appsignal_log_view.test"
	var appID, first, second string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             appIsGone,
		Steps: []resource.TestStep{
			{
				Config: appConfig(name) + fmt.Sprintf(logViewFull, "errors"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "errors"),
					resource.TestCheckResourceAttr(addr, "query", "severity:error"),
					resource.TestCheckResourceAttr(addr, "source_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "source_ids.0", "appsignal_log_source.test", "id"),
					resource.TestCheckResourceAttr(addr, "columns.#", "2"),
					resource.TestCheckResourceAttr(addr, "columns.0", "hostname"),
					resource.TestCheckResourceAttr(addr, "line_height", "1"),
					saveID(addr, &first),
					logViewExists(&appID, &first, true),
				),
			},
			{
				// Log views change in place.
				Config: appConfig(name) + fmt.Sprintf(logViewFull, "failures"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "failures"),
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
				Config: appConfig(name) + logViewMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, true),
					resource.TestCheckResourceAttr(addr, "query", ""),
					resource.TestCheckResourceAttr(addr, "source_ids.#", "0"),
					resource.TestCheckResourceAttr(addr, "columns.#", "0"),
					resource.TestCheckResourceAttr(addr, "line_height", "0"),
					func(s *terraform.State) error {
						v, err := testClient().GetLogView(context.Background(), appID, first)
						if err != nil {
							return err
						}
						if v.Query != "" || len(v.SourceIDs) != 0 || len(v.Columns) != 0 || v.LineHeight != "0" {
							return fmt.Errorf("AppSignal kept old values: %+v", v)
						}
						return nil
					},
				),
			},
			{
				// A log view deleted outside tofu is created again.
				PreConfig: func() {
					if err := testClient().DeleteLogView(context.Background(), appID, first); err != nil {
						t.Fatal(err)
					}
				},
				Config: appConfig(name) + logViewMinimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					idIs(addr, &first, false),
					saveID(addr, &second),
					logViewExists(&appID, &second, true),
				),
			},
			{
				Config: appConfig(name),
				Check:  logViewExists(&appID, &second, false),
			},
		},
	})
}
