package remote_device_svc

import "context"

func (s *service) List(ctx context.Context) ([]*DeviceView, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*DeviceView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toView(r))
	}
	return out, nil
}
