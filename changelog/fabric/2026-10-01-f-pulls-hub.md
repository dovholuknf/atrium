The hub carries a room's pulls API (`/v1/prs...`) and its `pr` event to the board. With one room addressed it is the byte
pipe it already was, and a new review on a room marked for deletion is refused like a launch is. In the ALL view
`GET /v1/prs` is merged (rows tagged `room~pr_...`, counts and `nav_count` summed, newest first, a room that errors in
`rooms_quiet`, one without pulls in `rooms_without`), `POST /v1/prs` asks which room, and `/v1/prs/{id}...` goes to the
room that holds the row, tagged or looked for. The `pr` event's `id` and `walker_task` are tagged like a card's.
