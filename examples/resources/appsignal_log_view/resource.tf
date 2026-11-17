resource "appsignal_log_view" "vector" {
  app_id     = appsignal_app.web.id
  name       = "AWS ECS containers Vector"
  source_ids = [appsignal_log_source.vector.id]
}
