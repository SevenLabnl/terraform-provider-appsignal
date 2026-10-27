resource "appsignal_log_metric" "jobs" {
  app_id     = appsignal_app.web.id
  name       = "Job metrics"
  query      = "message:job_finished"
  drop_lines = true
  metrics = [
    { name = "jobs.finished", type = "counter", tags = { queue = "default" } },
    { name = "jobs.duration", type = "distribution", field = "duration_ms" },
  ]
}
