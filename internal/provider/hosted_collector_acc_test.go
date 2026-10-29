package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Reads only: the API can create hosted collectors but not delete them, so the test
// needs an organization that already has a default one.
func TestAccHostedCollectorDataSource(t *testing.T) {
	org := os.Getenv("APPSIGNAL_ORGANIZATION")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "appsignal_hosted_collector" "default" {
  organization_slug = %[1]q
}

data "appsignal_hosted_collector" "by_id" {
  organization_slug = %[1]q
  id                = data.appsignal_hosted_collector.default.id
}
`, org),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.appsignal_hosted_collector.default", "is_default", "true"),
					resource.TestMatchResourceAttr("data.appsignal_hosted_collector.default", "url",
						regexp.MustCompile(`^https://[a-z0-9]+\.[a-z0-9-]+\.appsignal-collector\.net$`)),
					resource.TestCheckResourceAttrSet("data.appsignal_hosted_collector.default", "region"),
					resource.TestCheckResourceAttrSet("data.appsignal_hosted_collector.default", "subdomain"),
					resource.TestCheckResourceAttrSet("data.appsignal_hosted_collector.default", "status"),
					resource.TestCheckResourceAttrSet("data.appsignal_hosted_collector.default", "experimental"),
					resource.TestCheckResourceAttrPair("data.appsignal_hosted_collector.by_id", "url",
						"data.appsignal_hosted_collector.default", "url"),
				),
			},
			{
				Config: fmt.Sprintf(`
data "appsignal_hosted_collector" "missing" {
  organization_slug = %q
  id                = "000000000000000000000000"
}
`, org),
				ExpectError: regexp.MustCompile(`no hosted collector "000000000000000000000000"`),
			},
		},
	})
}
