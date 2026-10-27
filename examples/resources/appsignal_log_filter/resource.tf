resource "appsignal_log_filter" "health_checks" {
  app_id = appsignal_app.web.id
  name   = "Health checks"
  query  = "message:\"GET /healthz\""
}
