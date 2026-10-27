data "appsignal_app" "web" {
  organization_slug = "example"
  name              = "web"
  environment       = "production"
}

output "notifiers" {
  value = { for n in data.appsignal_app.web.notifiers : n.name => n.id }
}
