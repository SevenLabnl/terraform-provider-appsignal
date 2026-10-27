resource "appsignal_check_in" "nightly_export" {
  app_id            = appsignal_app.web.id
  identifier        = "nightly-export"
  kind              = "cron"
  schedule          = "0 2 * * *"
  timezone          = "Europe/Amsterdam"
  wait_time_minutes = 15
  notifier_ids      = [data.appsignal_notifier.email.id]
}

resource "appsignal_check_in" "worker" {
  app_id            = appsignal_app.web.id
  identifier        = "queue-worker"
  kind              = "heartbeat"
  wait_time_minutes = 5
  notifier_ids      = [data.appsignal_notifier.email.id]
}
