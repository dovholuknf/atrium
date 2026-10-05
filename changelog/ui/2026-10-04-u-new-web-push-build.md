# Phone alerts: the board half of web push

The notifications sheet on `/m` has a "phone alerts" switch. A tap asks for notification permission, reads the hub's
public key, subscribes the browser and hands the subscription to the hub. A second tap undoes it, with the endpoint as
proof it is this browser's. The row says why when it cannot work: a page that is not secure, an iPhone tab that is not
on the home screen, a hub with push off, a denied permission, or a browser that will not subscribe (Brave's setting is
named). The service worker now has a `push` handler that always shows a notification, with no action buttons, and a tap
opens the card's `/m` path. The hub half is not built yet, so the switch finds no key and says push is not turned on at
the hub.
