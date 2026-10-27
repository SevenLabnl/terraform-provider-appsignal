resource "appsignal_dashboard" "workers" {
  app_id      = appsignal_app.web.id
  title       = "Workers"
  description = "Queue depth and job throughput"
}
