// A real mouse on the private hidden KWin screen (tests/kwin/screen.sh), for
// checks that need the compositor to see presses: dragging a window by its
// title bar, double-clicks, right-click menus. It asks KWin for an input
// connection over the session bus (org.kde.KWin.EIS.RemoteDesktop) and sends
// libei pointer events. Commands on stdin, one per line:
//   move X Y      the pointer to screen point X, Y (absolute)
//   down|up B     button B: 1 left, 2 middle, 3 right
//   sleep MS
// It prints "ready" once KWin has given it a pointer, "ok" after each command.
// Build: cc -O2 -o ei-mouse ei-mouse.c $(pkg-config --cflags --libs libei-1.0 libsystemd)
#include <libei.h>
#include <linux/input-event-codes.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <systemd/sd-bus.h>
#include <time.h>
#include <unistd.h>

static struct ei *ei;
static struct ei_device *absolute, *buttons;
static int resumed_abs, resumed_btn;
static uint32_t sequence = 1;

static void pump(int timeout_ms) {
  struct pollfd fd = {.fd = ei_get_fd(ei), .events = POLLIN};
  if (poll(&fd, 1, timeout_ms) > 0) ei_dispatch(ei);
  struct ei_event *event;
  while ((event = ei_get_event(ei))) {
    struct ei_device *device = ei_event_get_device(event);
    switch (ei_event_get_type(event)) {
    case EI_EVENT_SEAT_ADDED:
      ei_seat_bind_capabilities(ei_event_get_seat(event), EI_DEVICE_CAP_POINTER_ABSOLUTE, EI_DEVICE_CAP_POINTER,
                                EI_DEVICE_CAP_BUTTON, EI_DEVICE_CAP_SCROLL, NULL);
      break;
    case EI_EVENT_DEVICE_ADDED:
      if (!absolute && ei_device_has_capability(device, EI_DEVICE_CAP_POINTER_ABSOLUTE)) absolute = ei_device_ref(device);
      if (!buttons && ei_device_has_capability(device, EI_DEVICE_CAP_BUTTON) &&
          (ei_device_has_capability(device, EI_DEVICE_CAP_POINTER_ABSOLUTE) || !absolute))
        buttons = ei_device_ref(device);
      break;
    case EI_EVENT_DEVICE_RESUMED:
      ei_device_start_emulating(device, sequence++);
      if (device == absolute) resumed_abs = 1;
      if (device == buttons) resumed_btn = 1;
      break;
    case EI_EVENT_DISCONNECT:
      fprintf(stderr, "ei-mouse: KWin closed the connection\n");
      exit(1);
    default:
      break;
    }
    ei_event_unref(event);
  }
}

int main(void) {
  sd_bus *bus = NULL;
  sd_bus_message *reply = NULL;
  sd_bus_error error = SD_BUS_ERROR_NULL;
  if (sd_bus_open_user(&bus) < 0) { fprintf(stderr, "ei-mouse: no session bus\n"); return 1; }
  // Capabilities as the remote desktop portal counts them: 1 keyboard, 2 pointer, 4 touch.
  if (sd_bus_call_method(bus, "org.kde.KWin", "/org/kde/KWin/EIS/RemoteDesktop", "org.kde.KWin.EIS.RemoteDesktop",
                         "connectToEIS", &error, &reply, "i", 2) < 0) {
    fprintf(stderr, "ei-mouse: KWin refused: %s\n", error.message);
    return 1;
  }
  int fd = -1, cookie = 0;
  if (sd_bus_message_read(reply, "hi", &fd, &cookie) < 0) { fprintf(stderr, "ei-mouse: odd answer\n"); return 1; }
  fd = dup(fd);
  ei = ei_new_sender(NULL);
  ei_configure_name(ei, "crush-desktop check");
  if (ei_setup_backend_fd(ei, fd) != 0) { fprintf(stderr, "ei-mouse: no connection\n"); return 1; }
  for (int i = 0; i < 200 && !(absolute && buttons && resumed_abs && resumed_btn); i++) pump(25);
  if (!absolute || !buttons) { fprintf(stderr, "ei-mouse: KWin gave no pointer\n"); return 1; }
  printf("ready\n");
  fflush(stdout);
  char line[128];
  while (fgets(line, sizeof line, stdin)) {
    double x, y;
    int b, ms;
    if (sscanf(line, "move %lf %lf", &x, &y) == 2) {
      ei_device_pointer_motion_absolute(absolute, x, y);
      ei_device_frame(absolute, ei_now(ei));
    } else if (sscanf(line, "down %d", &b) == 1 || sscanf(line, "up %d", &b) == 1) {
      int code = b == 3 ? BTN_RIGHT : b == 2 ? BTN_MIDDLE : BTN_LEFT;
      ei_device_button_button(buttons, code, line[0] == 'd');
      ei_device_frame(buttons, ei_now(ei));
    } else if (sscanf(line, "sleep %d", &ms) == 1) {
      struct timespec t = {ms / 1000, (ms % 1000) * 1000000L};
      nanosleep(&t, NULL);
    } else if (strncmp(line, "quit", 4) == 0) {
      break;
    }
    pump(0);
    printf("ok\n");
    fflush(stdout);
  }
  ei_unref(ei);
  return 0;
}
