package services

import "fyms/internal/dto"

func EmitLibraryDeleted(item NotifyDeletedItem) {
	if n := GetNotifier(); n != nil {
		n.Submit(NotifyEvent{Event: NotifyEventLibraryDeleted, DeletedItem: &item})
	}
}

func EmitUserDataNotify(event, itemID, userID, userName string, userData *dto.UserDataRow) {
	if n := GetNotifier(); n != nil {
		n.Submit(NotifyEvent{
			Event:    event,
			ItemID:   itemID,
			User:     &NotifyUser{Name: userName, ID: userID},
			UserData: userData,
		})
	}
}

func EmitPlaybackNotify(event, itemID, userID, userName string, session *NotifySession, info *NotifyPlaybackInfo, userData *dto.UserDataRow) {
	if n := GetNotifier(); n != nil {
		n.Submit(NotifyEvent{
			Event:        event,
			ItemID:       itemID,
			User:         &NotifyUser{Name: userName, ID: userID},
			UserData:     userData,
			Session:      session,
			PlaybackInfo: info,
		})
	}
}
