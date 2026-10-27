resource "appsignal_dashboard_number" "failed_jobs" {
  app_id       = appsignal_app.web.id
  dashboard_id = appsignal_dashboard.workers.id
  title        = "Failed jobs"
  layout       = { x = 6, y = 0, w = 3, h = 2 }
  metric = {
    name      = "jobs_failed"
    field     = "counter"
    aggregate = "sum"
  }
}
