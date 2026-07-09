package store

import (
	"time"

	"jmessage/internal/model"
)

// UpsertDevice registers (or refreshes) a device for a user. Called on
// every WebSocket connect; last-writer-wins is fine here.
func (s *Store) UpsertDevice(uid, deviceID, name string) error {
	return s.putJSON(deviceKey(uid, deviceID), &model.Device{
		ID:       deviceID,
		Name:     name,
		LastSeen: time.Now().UnixMilli(),
	})
}

// TouchDevice refreshes a device's lastSeen (called on disconnect).
func (s *Store) TouchDevice(uid, deviceID string) error {
	var d model.Device
	if err := s.getJSON(deviceKey(uid, deviceID), &d); err != nil {
		return err
	}
	d.LastSeen = time.Now().UnixMilli()
	return s.putJSON(deviceKey(uid, deviceID), &d)
}

// LastSeen returns the most recent device activity for a user (0 if
// the user has never connected a registered device).
func (s *Store) LastSeen(uid string) (int64, error) {
	devs, err := s.ListDevices(uid)
	if err != nil {
		return 0, err
	}
	var last int64
	for _, d := range devs {
		if d.LastSeen > last {
			last = d.LastSeen
		}
	}
	return last, nil
}

// ListDevices returns a user's registered devices.
func (s *Store) ListDevices(uid string) ([]model.Device, error) {
	prefix := devicePrefix(uid)
	it, err := s.db.Scan(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out []model.Device
	for ; it.Valid(); it.Next() {
		var d model.Device
		if err := jsonUnmarshal(it.Value(), &d); err != nil {
			continue
		}
		out = append(out, d)
	}
	return out, it.Err()
}
