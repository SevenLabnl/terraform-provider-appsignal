# A monitor made elsewhere (AppSignal UI, an operator), for example to show on a status page.
data "appsignal_uptime_monitor" "web" {
  app_id = data.appsignal_app.web.id
  name   = "www.example.com"
}
