#!/usr/bin/env python3
"""A stand-in notification server for tests (desktop-design-5-6 8.2). It owns
org.freedesktop.Notifications on the private session bus that
tests/driver/apps.py's Bus starts, and answers:
- GetCapabilities: ["body"];
- GetServerInformation: "crushtest-catcher", "crush-tests", "1", "1.2";
- Notify: appends {app_name, summary, body, at} as one JSON line to
  /tmp/crushtest-notifications.jsonl (or --log FILE) and returns a growing id;
- CloseNotification: nothing.
It shows nothing and plays nothing. Python with gi (Gio).

Usage: notify_catcher.py [--log FILE] [--ready FILE]
--ready names a file written once the name is owned."""
import argparse, json, sys, time

from gi.repository import Gio, GLib

NAME = "org.freedesktop.Notifications"
PATH = "/org/freedesktop/Notifications"
XML = """
<node>
  <interface name="org.freedesktop.Notifications">
    <method name="GetCapabilities"><arg direction="out" type="as"/></method>
    <method name="GetServerInformation">
      <arg direction="out" type="s"/><arg direction="out" type="s"/>
      <arg direction="out" type="s"/><arg direction="out" type="s"/>
    </method>
    <method name="Notify">
      <arg direction="in" type="s" name="app_name"/>
      <arg direction="in" type="u" name="replaces_id"/>
      <arg direction="in" type="s" name="app_icon"/>
      <arg direction="in" type="s" name="summary"/>
      <arg direction="in" type="s" name="body"/>
      <arg direction="in" type="as" name="actions"/>
      <arg direction="in" type="a{sv}" name="hints"/>
      <arg direction="in" type="i" name="expire_timeout"/>
      <arg direction="out" type="u"/>
    </method>
    <method name="CloseNotification"><arg direction="in" type="u"/></method>
    <signal name="NotificationClosed"><arg type="u"/><arg type="u"/></signal>
    <signal name="ActionInvoked"><arg type="u"/><arg type="s"/></signal>
  </interface>
</node>
"""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--log", default="/tmp/crushtest-notifications.jsonl")
    ap.add_argument("--ready")
    a = ap.parse_args()
    info = Gio.DBusNodeInfo.new_for_xml(XML).interfaces[0]
    state = {"next": 0}

    def on_call(_conn, _sender, _path, _iface, method, params, invocation):
        if method == "GetCapabilities":
            invocation.return_value(GLib.Variant("(as)", (["body"],)))
        elif method == "GetServerInformation":
            invocation.return_value(GLib.Variant("(ssss)", ("crushtest-catcher", "crush-tests", "1", "1.2")))
        elif method == "Notify":
            app_name, _replaces, _icon, summary, body = params.unpack()[:5]
            state["next"] += 1
            with open(a.log, "a") as f:
                f.write(json.dumps({"app_name": app_name, "summary": summary, "body": body, "at": time.time()}) + "\n")
            invocation.return_value(GLib.Variant("(u)", (state["next"],)))
        else:
            invocation.return_value(None)

    def on_bus(conn, _name):
        conn.register_object(PATH, info, on_call, None, None)

    def on_name(_conn, _name):
        if a.ready:
            with open(a.ready, "w") as f:
                f.write("ready\n")

    def on_lost(_conn, _name):
        print("notify_catcher: could not own " + NAME, file=sys.stderr)
        loop.quit()

    loop = GLib.MainLoop()
    Gio.bus_own_name(Gio.BusType.SESSION, NAME, Gio.BusNameOwnerFlags.NONE, on_bus, on_name, on_lost)
    loop.run()
    return 1


if __name__ == "__main__":
    sys.exit(main())
