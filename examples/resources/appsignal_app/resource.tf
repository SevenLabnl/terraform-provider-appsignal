resource "appsignal_app" "web" {
  organization_slug = "example"
  name              = "web"
  environment       = "production"

  lifecycle {
    # Destroying an app deletes all of its data.
    prevent_destroy = true
  }
}
