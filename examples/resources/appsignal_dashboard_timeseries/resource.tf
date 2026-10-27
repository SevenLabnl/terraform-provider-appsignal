resource "appsignal_dashboard_timeseries" "queue_depth" {
  app_id       = appsignal_app.web.id
  dashboard_id = appsignal_dashboard.workers.id
  title        = "Queue depth"
  display      = "area"
  line_label   = "%queue%"
  layout       = { x = 0, y = 0, w = 6, h = 4 }
  metrics = [
    { name = "queue_depth", fields = ["gauge"], tags = { queue = "*" } },
  ]
}
