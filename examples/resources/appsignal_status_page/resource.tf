resource "appsignal_status_page" "public" {
  organization_slug  = "example"
  hostname           = "status.example.com" # needs a CNAME to cname.appsignal-status.com first
  title              = "Example status"
  threshold          = 1
  uptime_monitor_ids = [appsignal_uptime_monitor.homepage.id]
}
