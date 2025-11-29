package node

import (
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/task"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) startTasks(node *panel.NodeInfo) {
	// fetch node info task
	c.nodeInfoMonitorPeriodic = &task.Task{
		Interval: node.PullInterval,
		Execute:  c.nodeInfoMonitor,
	}
	// fetch user list task
	c.userReportPeriodic = &task.Task{
		Interval: node.PushInterval,
		Execute:  c.reportUserTrafficTask,
	}

	log.WithField("tag", c.tag).Info("Start monitor node status")
	// delay to start nodeInfoMonitor
	_ = c.nodeInfoMonitorPeriodic.Start(false)

	log.WithField("tag", c.tag).Info("Start report node status")
	_ = c.userReportPeriodic.Start(false)

	if node.Security == panel.Tls {
		switch c.CertConfig.CertMode {
		case "none", "", "file", "self":
			// Do nothing for these modes
		default:
			c.renewCertPeriodic = &task.Task{
				Interval: time.Hour * 24,
				Execute:  c.renewCertTask,
			}
			log.WithField("tag", c.tag).Info("Start renew cert")
			// delay to start renewCert
			_ = c.renewCertPeriodic.Start(true)
		}
	}

	if c.LimitConfig.EnableDynamicSpeedLimit {
		c.traffic = make(map[string]int64)
		c.dynamicSpeedLimitPeriodic = &task.Task{
			Interval: time.Duration(c.LimitConfig.DynamicSpeedLimitConfig.Periodic) * time.Second,
			Execute:  c.SpeedChecker,
		}
		log.WithField("tag", c.tag).Info("Start dynamic speed limit")
	}
}

func (c *Controller) nodeInfoMonitor() (err error) {
	// get node info
	newNode, err := c.apiClient.GetNodeInfo()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get node info failed")
		return nil
	}

	// get user info
	newUsers, err := c.apiClient.GetUserList()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get user list failed")
		return nil
	}

	// get user alive info
	newAlive, err := c.apiClient.GetUserAlive()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Warn("Get alive list failed, continuing with empty list")
		newAlive = make(map[int]int) // Continue with empty map
	}

	// If node info changed
	if newNode != nil {
		c.info = newNode

		// Update user list
		if newUsers != nil {
			c.userList = newUsers
		}

		// Reset traffic records
		c.traffic = make(map[string]int64)

		// Remove old node
		log.WithField("tag", c.tag).Info("Node changed, reload")
		err = c.server.DelNode(c.tag)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete node failed")
			return nil
		}

		// Handle tag and limiter updates
		oldTag := c.tag
		if c.Options.Name == "" {
			c.tag = c.buildNodeTag(newNode)

			// Remove old limiter
			limiter.DeleteLimiter(oldTag)

			// Add new Limiter
			l := limiter.AddLimiter(c.tag, &c.LimitConfig, c.userList, newAlive)
			c.limiter = l
		} else {
			// Update existing limiter with new alive list
			c.limiter.AliveList = newAlive
		}

		// Update rules
		err = c.limiter.UpdateRule(&newNode.Rules)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Update Rule failed")
			return nil
		}

		// Handle certificate if needed
		if newNode.Security == panel.Tls {
			err = c.requestCert()
			if err != nil {
				log.WithFields(log.Fields{
					"tag": c.tag,
					"err": err,
				}).Error("Request cert failed")
				return nil
			}
		}

		// Add new node
		err = c.server.AddNode(c.tag, newNode, c.Options)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add node failed")
			return nil
		}

		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			Users:    c.userList,
			NodeInfo: newNode,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return nil
		}

		// Update intervals if needed
		if c.nodeInfoMonitorPeriodic.Interval != newNode.PullInterval && newNode.PullInterval != 0 {
			c.nodeInfoMonitorPeriodic.Interval = newNode.PullInterval
			c.nodeInfoMonitorPeriodic.Close()
			_ = c.nodeInfoMonitorPeriodic.Start(false)
		}

		if c.userReportPeriodic.Interval != newNode.PushInterval && newNode.PushInterval != 0 {
			c.userReportPeriodic.Interval = newNode.PushInterval
			c.userReportPeriodic.Close()
			_ = c.userReportPeriodic.Start(false)
		}

		log.WithField("tag", c.tag).Infof("Added %d new users", len(c.userList))
		return nil
	}

	// Node didn't change, but update alive list if available
	if newAlive != nil {
		c.limiter.AliveList = newAlive
	}

	// Check users if we have a new user list
	if len(newUsers) == 0 {
		return nil
	}

	deleted, added := compareUserList(c.userList, newUsers)
	if len(deleted) > 0 {
		// Delete users
		err = c.server.DelUsers(deleted, c.tag, c.info)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete users failed")
			return nil
		}
	}

	if len(added) > 0 {
		// Add users
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			NodeInfo: c.info,
			Users:    added,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return nil
		}
	}

	// Update limiter if users changed
	if len(added)+len(deleted) > 0 {
		c.limiter.UpdateUser(c.tag, added, deleted)

		// Clear traffic records for deleted users
		if c.LimitConfig.EnableDynamicSpeedLimit {
			for i := range deleted {
				delete(c.traffic, deleted[i].Uuid)
			}
		}

		log.WithField("tag", c.tag).
			Infof("%d user deleted, %d user added", len(deleted), len(added))
	}

	// Update user list
	c.userList = newUsers
	return nil
}

func (c *Controller) SpeedChecker() error {
	for uuid, traffic := range c.traffic {
		if traffic >= c.LimitConfig.DynamicSpeedLimitConfig.Traffic {
			expireTime := time.Now().Add(time.Duration(c.LimitConfig.DynamicSpeedLimitConfig.ExpireTime) * time.Minute)
			err := c.limiter.UpdateDynamicSpeedLimit(c.tag, uuid,
				c.LimitConfig.DynamicSpeedLimitConfig.SpeedLimit,
				expireTime)

			if err != nil {
				log.WithFields(log.Fields{
					"tag":  c.tag,
					"uuid": uuid,
					"err":  err,
				}).Error("Update dynamic speed limit failed")
			} else {
				// Successfully applied limit, remove from tracking
				delete(c.traffic, uuid)
			}
		}
	}
	return nil
}
