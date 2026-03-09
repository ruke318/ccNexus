package service

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/codexpool"
	"github.com/lich0821/ccNexus/internal/logger"
)

type CodexPoolService struct {
	manager  *codexpool.Manager
	terminal *TerminalService
}

func NewCodexPoolService(manager *codexpool.Manager, terminal *TerminalService) *CodexPoolService {
	return &CodexPoolService{
		manager:  manager,
		terminal: terminal,
	}
}

func (s *CodexPoolService) ListSlots() string {
	slots, err := s.manager.ListSlots()
	return marshalServiceResult("slots", slots, err)
}

func (s *CodexPoolService) CreateSlot(name, accountID, remark string) string {
	slot, err := s.manager.CreateSlot(name, accountID, remark)
	return marshalServiceResult("slot", slot, err)
}

func (s *CodexPoolService) UpdateSlot(id int64, name, accountID, remark string, enabled bool) string {
	slot, err := s.manager.UpdateSlot(id, name, accountID, remark, enabled)
	return marshalServiceResult("slot", slot, err)
}

func (s *CodexPoolService) DeleteSlot(id int64) error {
	return s.manager.DeleteSlot(id)
}

func (s *CodexPoolService) SyncSlotStatus(id int64) string {
	slot, err := s.manager.SyncSlotStatus(id)
	return marshalServiceResult("slot", slot, err)
}

func (s *CodexPoolService) LaunchSlotLogin(id int64, openBrowser func(string)) error {
	return s.manager.StartSlotAuth(id, openBrowser)
}

func (s *CodexPoolService) ListPools() string {
	pools, err := s.manager.ListPools()
	return marshalServiceResult("pools", pools, err)
}

func (s *CodexPoolService) CreatePool(name string, slotIDs []int64) string {
	pool, err := s.manager.CreatePool(name, slotIDs)
	return marshalServiceResult("pool", pool, err)
}

func (s *CodexPoolService) UpdatePool(id int64, name, strategy string, enabled bool, cooldown429Sec, cooldown5xxSec int, authExpiredPolicy string, slotIDs []int64) string {
	pool, err := s.manager.UpdatePool(id, name, strategy, enabled, cooldown429Sec, cooldown5xxSec, authExpiredPolicy, slotIDs)
	return marshalServiceResult("pool", pool, err)
}

func (s *CodexPoolService) DeletePool(id int64) error {
	return s.manager.DeletePool(id)
}

func marshalServiceResult(key string, payload interface{}, err error) string {
	result := map[string]interface{}{
		"success": err == nil,
	}
	if err != nil {
		result["message"] = err.Error()
		logger.Error("Codex pool service failed: %v", err)
	} else {
		result[key] = payload
	}
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		fallback := map[string]interface{}{
			"success": false,
			"message": marshalErr.Error(),
		}
		data, _ = json.Marshal(fallback)
	}
	return string(data)
}
