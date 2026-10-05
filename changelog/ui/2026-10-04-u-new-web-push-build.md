# Phone alerts: the board half of web push

The notifications sheet on `/m` has a "phone alerts" switch. A tap asks for notification permission, reads the hub's
public key, subscribes the browser and hands the subscription to the hub. A second tap undoes it, with the endpoint as
proof it is this browser's. The row says why when it cannot work: a page that is not secure, an iPhone tab that is not
on the home screen, a hub with push off, a denied permission, or a browser that will not subscribe (Brave's setting is
named). The service worker now has a `push` handler that always shows a notification, with no action buttons, and a tap
opens the card's `/m` path. The hub half is not built yet, so the switch finds no key and says push is not turned on at
the hub.

## The hub half

The hub now sends the push. Turn it on with `PUT /_hub/push` from the machine the hub runs on. It makes its own key,
stores the phones that subscribe (at most 8, never evicting one), and sends each alert encrypted to the phone, with the
same four fields and the same rules as the board's other alerts. A burst of more than 5 in 2 minutes becomes one
summary. A phone the push service says is gone is dropped, and three failures in a row switch one phone off. A new
device shows in the desktop growler. The desktop gear list is still to come.

## The desktop gear and the phone name

The notifications pane of the gear has a "phone alerts (on the hub)" row. It has the on and off switch, the contact the
push service can reach, a list of the subscribed phones (name, page, push service, age, failures, and why one was
switched off) with a remove button each, a test to every phone and a new key, which asks first because it removes every
phone. It shows only on the hub machine itself and is hidden for a guest. The phone can now name itself in the
notifications sheet. A rename does not buzz the phone with a second test push.
