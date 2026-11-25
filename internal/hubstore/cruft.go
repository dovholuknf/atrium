package hubstore

import "strings"

// A ROOM'S LATEST CRUFT REPORT, kept as a hub setting named cruft_report/<room> so it needs no migration. The hub keeps
// the latest per room and nothing older: a report is what the room's nightly pass did, never state anything reads back.

const settingCruftPrefix = "cruft_report/"

// PutCruftReport replaces the room's report with raw, a JSON object the caller has checked.
func (s *Store) PutCruftReport(room, raw string) error {
	return s.SetHubSetting(settingCruftPrefix+room, raw)
}

// CruftReport is the room's latest report, "" when it has made none.
func (s *Store) CruftReport(room string) (string, error) {
	return s.HubSetting(settingCruftPrefix + room)
}

// CruftReports is every room's latest report by room name.
func (s *Store) CruftReports() (map[string]string, error) {
	out := map[string]string{}
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT name, value FROM hub_setting WHERE name LIKE ?`, settingCruftPrefix+"%")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name, v string
			if err := rows.Scan(&name, &v); err != nil {
				return err
			}
			out[strings.TrimPrefix(name, settingCruftPrefix)] = v
		}
		return rows.Err()
	})
	return out, err
}
