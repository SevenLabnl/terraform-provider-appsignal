# Notifiers can only be created in the AppSignal UI; look them up by name.
data "appsignal_notifier" "slack" {
  app_id = data.appsignal_app.web.id
  name   = "Slack #alerts"
}
