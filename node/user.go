package node

import (
	"strconv"

	"github.com/InazumaV/V2bX/api/panel"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) reportUserTrafficTask() (err error) {
	userTraffic, _ := c.server.GetUserTrafficSlice(c.tag, true)
	if len(userTraffic) > 0 {
		err = c.apiClient.ReportUserTraffic(userTraffic)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Warn("Report user traffic failed")
		} else {
			log.WithField("tag", c.tag).Infof("Report %d users traffic", len(userTraffic))
			log.WithField("tag", c.tag).Debugf("User traffic: %+v", userTraffic)
		}
	}

	if onlineDevice, err := c.limiter.GetOnlineDevice(); err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Warn("Failed to get online device list")
	} else if len(*onlineDevice) > 0 {
		// Only report user has traffic > min traffic to allow ping test
		var result []panel.OnlineUser
		var nocountUID = make(map[int]struct{})

		// Identify users with low traffic
		for _, traffic := range userTraffic {
			total := traffic.Upload + traffic.Download
			minTraffic := int64(c.Options.DeviceOnlineMinTraffic * 1000)
			if total < minTraffic {
				nocountUID[traffic.UID] = struct{}{}
			}
		}

		// Filter online users
		for _, online := range *onlineDevice {
			if _, ok := nocountUID[online.UID]; !ok {
				result = append(result, online)
			}
		}

		// Prepare data for reporting
		data := make(map[int][]string)
		for _, onlineuser := range result {
			// json structure: { UID1:["ip1","ip2"],UID2:["ip3","ip4"] }
			data[onlineuser.UID] = append(data[onlineuser.UID], onlineuser.IP)
		}

		if len(data) > 0 {
			if err = c.apiClient.ReportNodeOnlineUsers(&data); err != nil {
				log.WithFields(log.Fields{
					"tag": c.tag,
					"err": err,
				}).Warn("Report online users failed")
			} else {
				log.WithField("tag", c.tag).Infof("Total %d online users, %d reported", len(*onlineDevice), len(result))
				log.WithField("tag", c.tag).Debugf("Online users: %+v", data)
			}
		}
	}

	// Explicitly set to nil to help GC
	userTraffic = nil
	return nil
}

func compareUserList(old, new []panel.UserInfo) (deleted, added []panel.UserInfo) {
	// Create a map for fast lookup of old users
	oldMap := make(map[string]panel.UserInfo)
	for _, user := range old {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		oldMap[key] = user
	}

	// Check for added or unchanged users
	newMap := make(map[string]panel.UserInfo)
	for _, user := range new {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		newMap[key] = user

		if _, exists := oldMap[key]; !exists {
			added = append(added, user)
		}
	}

	// Check for deleted users
	for key, user := range oldMap {
		if _, exists := newMap[key]; !exists {
			deleted = append(deleted, user)
		}
	}

	return deleted, added
}
