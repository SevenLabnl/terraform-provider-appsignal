data "appsignal_hosted_collector" "this" {
  organization_slug = "example"
}

# Point any OTLP/HTTP exporter at it. Exporters that cannot set resource attributes
# send the app name, environment and push API key as Appsignal-Config-* headers.
output "otlp_endpoint" {
  value = data.appsignal_hosted_collector.this.url
}
