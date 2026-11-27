import { HubConnectionBuilder, HttpTransportType, LogLevel } from "@microsoft/signalr"

const key = process.env.PANGOLIN_REALTIME_DEMO_KEY
if (!key) throw new Error("Set PANGOLIN_REALTIME_DEMO_KEY")
const url = process.argv[2] || "http://127.0.0.1:8088/hub"
const connection = new HubConnectionBuilder()
  .withUrl(url, {
    transport: HttpTransportType.WebSockets,
    accessTokenFactory: () => `${key}:alpha/alice`,
  })
  .withAutomaticReconnect()
  .configureLogging(LogLevel.None)
  .build()

const target = {tenant: "alpha", kind: "tenant", id: "alpha", channel: "tenant"}
const pushed = new Promise(resolve => connection.on("Notification", resolve))
try {
  await connection.start()
  console.log(await connection.invoke("Echo", "Hello SignalR"))
  await connection.invoke("Subscribe", target)
  const id = await connection.invoke("Notify", target, {text: "Committed notification"})
  const message = await Promise.race([
    pushed,
    new Promise((_, reject) => setTimeout(() => reject(new Error("No notification")), 5000).unref()),
  ])
  if (message.id !== id) throw new Error("Event ID mismatch")
  console.log(id, message.data.text)
} finally {
  await connection.stop()
}
