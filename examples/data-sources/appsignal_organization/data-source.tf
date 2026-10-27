data "appsignal_organization" "this" {
  slug = "example"
}

# For example, hand the push API key to a collector through a secret store.
output "push_api_key" {
  value     = data.appsignal_organization.this.push_api_key
  sensitive = true
}
