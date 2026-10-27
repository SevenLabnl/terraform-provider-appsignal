resource "appsignal_trigger" "queue_depth" {
  app_id              = appsignal_app.web.id
  kind                = "Advanced"
  name                = "Queue depth"
  description         = "Jobs are piling up; check the workers."
  metric_name         = "queue_depth"
  field               = "gauge"
  tags                = { queue = "default" }
  comparison_operator = ">"
  threshold           = 500
  warmup_duration     = 5
  cooldown_duration   = 5
  notifier_ids        = [data.appsignal_notifier.email.id]
}
