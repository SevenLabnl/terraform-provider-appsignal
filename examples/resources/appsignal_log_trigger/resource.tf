resource "appsignal_log_trigger" "import_errors" {
  app_id                 = appsignal_app.web.id
  name                   = "Import errors"
  query                  = "severity:error"
  source_ids             = [appsignal_log_source.workers.id]
  notifier_ids           = [data.appsignal_notifier.email.id]
  notification_frequency = "nth_in_hour"
  notification_threshold = 3
}
