resource "appsignal_log_source" "vector" {
  app_id         = appsignal_app.web.id
  name           = "vector"
  type           = "http_json"
  format         = "json"
  parse_severity = true
}
