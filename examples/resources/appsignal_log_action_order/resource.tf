# Drop health checks before they are counted or alerted on.
resource "appsignal_log_action_order" "web" {
  app_id = appsignal_app.web.id
  action_ids = [
    appsignal_log_filter.health_checks.id,
    appsignal_log_metric.jobs.id,
    appsignal_log_trigger.import_errors.id,
  ]
}
