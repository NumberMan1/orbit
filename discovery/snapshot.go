package discovery

import (
	"fmt"
	"maps"
	"strings"
)

// Snapshot is a caller-owned copy. Reading several nodes from the same Snapshot
// observes one revision; separate Get calls may span revisions.
type Snapshot struct {
	Revision int64
	Services map[string]Descriptor
}

func snapshotFromList(result ListResult, minimumRevision int64) (*Snapshot, error) {
	if result.Revision < 1 || result.Revision < minimumRevision {
		return nil, fmt.Errorf("%w: list revision regressed", ErrInvalidData)
	}
	services := make(map[string]Descriptor, len(result.Services))
	for _, d := range result.Services {
		if err := d.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidData, err)
		}
		if _, exists := services[d.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate service ID %s", ErrInvalidData, d.ID)
		}
		d.Metadata = maps.Clone(d.Metadata)
		services[d.ID] = d
	}
	return &Snapshot{Revision: result.Revision, Services: services}, nil
}

func applyBatch(current *Snapshot, batch Batch) (*Snapshot, error) {
	if batch.Revision < 1 {
		return nil, fmt.Errorf("%w: invalid batch revision", ErrInvalidData)
	}
	if batch.Revision <= current.Revision {
		return current, nil
	}
	if len(batch.Changes) == 0 {
		return &Snapshot{Revision: batch.Revision, Services: current.Services}, nil
	}
	// Validate the WHOLE transaction before changing or publishing anything.
	seen := make(map[string]bool, len(batch.Changes))
	for _, change := range batch.Changes {
		if change.ID == "" || strings.ContainsAny(change.ID, "/\\") || seen[change.ID] {
			return nil, fmt.Errorf("%w: invalid or repeated service ID", ErrInvalidData)
		}
		seen[change.ID] = true
		switch change.Type {
		case Put:
			if err := change.Service.Validate(); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidData, err)
			}
			if change.Service.ID != change.ID {
				return nil, fmt.Errorf("%w: key and service ID differ", ErrInvalidData)
			}
		case Delete:
		default:
			return nil, fmt.Errorf("%w: unknown change type", ErrInvalidData)
		}
	}
	// Previously published descriptors are immutable and may be shared internally.
	services := maps.Clone(current.Services)
	for _, change := range batch.Changes {
		if change.Type == Delete {
			delete(services, change.ID)
			continue
		}
		d := change.Service
		d.Metadata = maps.Clone(d.Metadata)
		services[change.ID] = d
	}
	return &Snapshot{Revision: batch.Revision, Services: services}, nil
}
