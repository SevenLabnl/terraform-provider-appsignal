data "appsignal_notifier" "email" {
  app_id = appsignal_app.web.id
  name   = "Default email notifier"
}

resource "appsignal_uptime_monitor" "homepage" {
  app_id                 = appsignal_app.web.id
  name                   = "www.example.com"
  url                    = "https://www.example.com/health"
  regions                = ["europe", "north_america"]
  notifier_ids           = [data.appsignal_notifier.email.id]
  warmup_duration        = 3
  check_body_content     = "ok"
  monitor_ssl_expiration = true
}
