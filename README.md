# terraform-provider-appsignal

An [OpenTofu](https://opentofu.org) and Terraform provider for
[AppSignal](https://www.appsignal.com), built on AppSignal's GraphQL API.

> Not affiliated with AppSignal.

```hcl
terraform {
  required_providers {
    appsignal = {
      source = "sevenlabnl/appsignal"
    }
  }
}

provider "appsignal" {} # token from APPSIGNAL_TOKEN

resource "appsignal_app" "web" {
  organization_slug = "example"
  name              = "web"
  environment       = "production"
}

resource "appsignal_uptime_monitor" "homepage" {
  app_id          = appsignal_app.web.id
  name            = "www.example.com"
  url             = "https://www.example.com/"
  regions         = ["europe"]
  warmup_duration = 3
}
```

## What it manages

| Resource | |
|---|---|
| `appsignal_app` | an app in one environment |
| `appsignal_log_source` | a log source in an app, with its ingestion key |
| `appsignal_uptime_monitor` | an uptime monitor |
| `appsignal_trigger` | a metric trigger (anomaly detection) |
| `appsignal_log_trigger` | a trigger on log lines matching a query |
| `appsignal_dashboard` | a dashboard |
| `appsignal_dashboard_timeseries` | a graph of metrics over time on a dashboard |
| `appsignal_dashboard_number` | a single-number tile on a dashboard |
| `appsignal_check_in` | a cron or heartbeat check-in monitor |
| `appsignal_log_filter` | drops matching log lines at ingestion |
| `appsignal_log_metric` | turns matching log lines into metrics |
| `appsignal_log_action_order` | the order log filters, metrics and log triggers run in |
| `appsignal_status_page` | a public status page of the organization |

| Data source | |
|---|---|
| `appsignal_organization` | organization by slug, with its push API key |
| `appsignal_app` | app by id or by name and environment, with its notifiers |
| `appsignal_notifier` | a notifier by name |

Full reference in [`docs/`](./docs).

## What the API does not allow

- **Notifiers cannot be created.** Make them in the AppSignal UI (Slack needs its OAuth
  flow there) and refer to them with the `appsignal_notifier` data source.
- **Apps cannot be renamed.** Changing `name` or `environment` replaces the app, and
  destroying an app deletes its data. Consider `prevent_destroy`.
- **Triggers are immutable.** Every change to an `appsignal_trigger` archives it and
  creates a new version with a new id, which closes the alerts open on the old one.
- **Most input is stored unchecked.** AppSignal accepts unknown formats, negative
  durations and log queries that never match; the provider rejects what it can at plan
  time, but check a log query in the log viewer before relying on it.
- **A status page hostname must resolve first.** AppSignal only accepts it once it has a
  CNAME to `cname.appsignal-status.com`.
- **Log actions run in order and new ones are added last.** Use
  `appsignal_log_action_order` when a filter has to run before a metric or trigger.
- **Dashboard-wide visual tags are not supported.** AppSignal fails with an internal
  error when a graph has them, so put tags on each metric instead.
- **Uptime checks run every minute with a 30 second timeout and expect a 2xx.** None of
  that is configurable.

## The token

An AppSignal **personal API token**, from *Personal settings → API tokens*. The GraphQL
API accepts nothing narrower, so the token has every right its user has. Use a
dedicated user where you can, and keep the token in a secret store, not in code.

## Developing

```bash
go test ./...                       # unit tests, no network
TF_ACC=1 APPSIGNAL_TOKEN=... APPSIGNAL_ORGANIZATION=... go test ./internal/provider/
go generate ./...                   # regenerate docs/
```

The acceptance tests create a real app named `tf-acc-*` in the organization, exercise
every resource and data source, and delete the app again. With OpenTofu instead of
Terraform, also set `TF_ACC_TERRAFORM_PATH=$(which tofu)`,
`TF_ACC_PROVIDER_HOST=registry.opentofu.org` and `TF_ACC_PROVIDER_NAMESPACE=sevenlabnl`.

## Licence

Apache 2.0.
