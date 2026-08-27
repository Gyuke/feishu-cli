package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	larkcalendar "github.com/larksuite/oapi-sdk-go/v3/service/calendar/v4"
)

// Calendar 日历信息
type Calendar struct {
	CalendarID   string `json:"calendar_id"`
	Summary      string `json:"summary"`
	Description  string `json:"description,omitempty"`
	Permissions  string `json:"permissions,omitempty"`
	Type         string `json:"type,omitempty"`
	Color        int    `json:"color,omitempty"`
	Role         string `json:"role,omitempty"`
	SummaryAlias string `json:"summary_alias,omitempty"`
	IsDeleted    bool   `json:"is_deleted,omitempty"`
	IsThirdParty bool   `json:"is_third_party,omitempty"`
}

// CalendarEvent 日程信息
type CalendarEvent struct {
	EventID     string `json:"event_id"`
	OrganizerID string `json:"organizer_calendar_id,omitempty"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	TimeZone    string `json:"time_zone,omitempty"`
	Location    string `json:"location,omitempty"`
	Status      string `json:"status,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
	CreateTime  string `json:"create_time,omitempty"`
	RecurringID string `json:"recurring_event_id,omitempty"`
	Recurrence  string `json:"recurrence,omitempty"` // 重复日程规则（RFC5545 RRULE）
	IsException bool   `json:"is_exception,omitempty"`
	AppLink     string `json:"app_link,omitempty"`
	Color       int    `json:"color,omitempty"`
}

// ListCalendars 列出日历
func ListCalendars(pageSize int, pageToken string, userAccessToken string) ([]*Calendar, string, bool, error) {
	client, err := GetClient()
	if err != nil {
		return nil, "", false, err
	}

	reqBuilder := larkcalendar.NewListCalendarReqBuilder()
	if pageSize > 0 {
		reqBuilder.PageSize(pageSize)
	}
	if pageToken != "" {
		reqBuilder.PageToken(pageToken)
	}

	resp, err := client.Calendar.Calendar.List(Context(), reqBuilder.Build(), UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, "", false, fmt.Errorf("获取日历列表失败: %w", err)
	}

	if !resp.Success() {
		return nil, "", false, fmt.Errorf("获取日历列表失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	var calendars []*Calendar
	if resp.Data != nil && resp.Data.CalendarList != nil {
		for _, item := range resp.Data.CalendarList {
			calendars = append(calendars, &Calendar{
				CalendarID:   StringVal(item.CalendarId),
				Summary:      StringVal(item.Summary),
				Description:  StringVal(item.Description),
				Permissions:  StringVal(item.Permissions),
				Type:         StringVal(item.Type),
				Color:        IntVal(item.Color),
				Role:         StringVal(item.Role),
				SummaryAlias: StringVal(item.SummaryAlias),
				IsDeleted:    BoolVal(item.IsDeleted),
				IsThirdParty: BoolVal(item.IsThirdParty),
			})
		}
	}

	var nextPageToken string
	var hasMore bool
	if resp.Data != nil {
		nextPageToken = StringVal(resp.Data.PageToken)
		hasMore = BoolVal(resp.Data.HasMore)
	}

	return calendars, nextPageToken, hasMore, nil
}

// CreateEventParams 创建日程的参数
type CreateEventParams struct {
	CalendarID  string
	Summary     string
	Description string
	StartTime   string // RFC3339 格式
	EndTime     string // RFC3339 格式
	TimeZone    string
	Location    string
	Recurrence  string // 重复日程规则（RFC5545 RRULE），如 FREQ=WEEKLY;BYDAY=MO
}

// CreateEvent 创建日程
func CreateEvent(params *CreateEventParams, userAccessToken string) (*CalendarEvent, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}

	startTs, err := parseTimeToTimestamp(params.StartTime)
	if err != nil {
		return nil, fmt.Errorf("解析开始时间失败: %w", err)
	}
	endTs, err := parseTimeToTimestamp(params.EndTime)
	if err != nil {
		return nil, fmt.Errorf("解析结束时间失败: %w", err)
	}

	startTime := larkcalendar.NewTimeInfoBuilder().
		Timestamp(startTs).
		Build()
	endTime := larkcalendar.NewTimeInfoBuilder().
		Timestamp(endTs).
		Build()

	eventBuilder := larkcalendar.NewCalendarEventBuilder().
		Summary(params.Summary).
		StartTime(startTime).
		EndTime(endTime)

	if params.Description != "" {
		eventBuilder.Description(params.Description)
	}

	if params.Location != "" {
		location := larkcalendar.NewEventLocationBuilder().
			Name(params.Location).
			Build()
		eventBuilder.Location(location)
	}

	if params.Recurrence != "" {
		eventBuilder.Recurrence(params.Recurrence)
	}

	req := larkcalendar.NewCreateCalendarEventReqBuilder().
		CalendarId(params.CalendarID).
		CalendarEvent(eventBuilder.Build()).
		Build()

	resp, err := client.Calendar.CalendarEvent.Create(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, fmt.Errorf("创建日程失败: %w", err)
	}

	if !resp.Success() {
		return nil, fmt.Errorf("创建日程失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || resp.Data.Event == nil {
		return nil, fmt.Errorf("创建日程成功但未返回日程信息")
	}

	return convertEvent(resp.Data.Event), nil
}

// GetEvent 获取日程详情
func GetEvent(calendarID, eventID string, userAccessToken string) (*CalendarEvent, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}

	req := larkcalendar.NewGetCalendarEventReqBuilder().
		CalendarId(calendarID).
		EventId(eventID).
		Build()

	resp, err := client.Calendar.CalendarEvent.Get(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, fmt.Errorf("获取日程详情失败: %w", err)
	}

	if !resp.Success() {
		return nil, fmt.Errorf("获取日程详情失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || resp.Data.Event == nil {
		return nil, fmt.Errorf("日程不存在")
	}

	return convertEvent(resp.Data.Event), nil
}

// ListEventsParams 列出日程的参数
type ListEventsParams struct {
	CalendarID string
	StartTime  string // RFC3339 格式，可选
	EndTime    string // RFC3339 格式，可选
	PageSize   int
	PageToken  string
}

// ListEvents 列出日程
func ListEvents(params *ListEventsParams, userAccessToken string) ([]*CalendarEvent, string, bool, error) {
	client, err := GetClient()
	if err != nil {
		return nil, "", false, err
	}

	reqBuilder := larkcalendar.NewListCalendarEventReqBuilder().
		CalendarId(params.CalendarID)

	if params.StartTime != "" {
		startTs, err := parseTimeToTimestamp(params.StartTime)
		if err != nil {
			return nil, "", false, fmt.Errorf("解析开始时间失败: %w", err)
		}
		reqBuilder.StartTime(startTs)
	}

	if params.EndTime != "" {
		endTs, err := parseTimeToTimestamp(params.EndTime)
		if err != nil {
			return nil, "", false, fmt.Errorf("解析结束时间失败: %w", err)
		}
		reqBuilder.EndTime(endTs)
	}

	if params.PageSize > 0 {
		reqBuilder.PageSize(params.PageSize)
	}

	if params.PageToken != "" {
		reqBuilder.PageToken(params.PageToken)
	}

	resp, err := client.Calendar.CalendarEvent.List(Context(), reqBuilder.Build(), UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, "", false, fmt.Errorf("获取日程列表失败: %w", err)
	}

	if !resp.Success() {
		return nil, "", false, fmt.Errorf("获取日程列表失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	var events []*CalendarEvent
	if resp.Data != nil && resp.Data.Items != nil {
		for _, item := range resp.Data.Items {
			events = append(events, convertEvent(item))
		}
	}

	var nextPageToken string
	var hasMore bool
	if resp.Data != nil {
		nextPageToken = StringVal(resp.Data.PageToken)
		hasMore = BoolVal(resp.Data.HasMore)
	}

	return events, nextPageToken, hasMore, nil
}

// UpdateEventParams 更新日程的参数
type UpdateEventParams struct {
	CalendarID  string
	EventID     string
	Summary     string
	Description string
	StartTime   string // RFC3339 格式
	EndTime     string // RFC3339 格式
	Location    string
	Recurrence  string // 重复日程规则（RFC5545 RRULE），如 FREQ=WEEKLY;BYDAY=MO
}

// UpdateEvent 更新日程（使用 Patch 方式）
func UpdateEvent(params *UpdateEventParams, userAccessToken string) (*CalendarEvent, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}

	eventBuilder := larkcalendar.NewCalendarEventBuilder()

	if params.Summary != "" {
		eventBuilder.Summary(params.Summary)
	}

	if params.Description != "" {
		eventBuilder.Description(params.Description)
	}

	if params.StartTime != "" {
		startTs, err := parseTimeToTimestamp(params.StartTime)
		if err != nil {
			return nil, fmt.Errorf("解析开始时间失败: %w", err)
		}
		startTime := larkcalendar.NewTimeInfoBuilder().
			Timestamp(startTs).
			Build()
		eventBuilder.StartTime(startTime)
	}

	if params.EndTime != "" {
		endTs, err := parseTimeToTimestamp(params.EndTime)
		if err != nil {
			return nil, fmt.Errorf("解析结束时间失败: %w", err)
		}
		endTime := larkcalendar.NewTimeInfoBuilder().
			Timestamp(endTs).
			Build()
		eventBuilder.EndTime(endTime)
	}

	if params.Location != "" {
		location := larkcalendar.NewEventLocationBuilder().
			Name(params.Location).
			Build()
		eventBuilder.Location(location)
	}

	if params.Recurrence != "" {
		eventBuilder.Recurrence(params.Recurrence)
	}

	req := larkcalendar.NewPatchCalendarEventReqBuilder().
		CalendarId(params.CalendarID).
		EventId(params.EventID).
		CalendarEvent(eventBuilder.Build()).
		Build()

	resp, err := client.Calendar.CalendarEvent.Patch(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, fmt.Errorf("更新日程失败: %w", err)
	}

	if !resp.Success() {
		return nil, fmt.Errorf("更新日程失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || resp.Data.Event == nil {
		return nil, fmt.Errorf("更新日程成功但未返回日程信息")
	}

	return convertEvent(resp.Data.Event), nil
}

// DeleteEvent 删除日程
func DeleteEvent(calendarID, eventID string, userAccessToken string) error {
	client, err := GetClient()
	if err != nil {
		return err
	}

	req := larkcalendar.NewDeleteCalendarEventReqBuilder().
		CalendarId(calendarID).
		EventId(eventID).
		Build()

	resp, err := client.Calendar.CalendarEvent.Delete(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return fmt.Errorf("删除日程失败: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("删除日程失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	return nil
}

// 辅助函数：将 RFC3339 时间格式转换为时间戳字符串
func parseTimeToTimestamp(timeStr string) (string, error) {
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(t.Unix(), 10), nil
}

// 辅助函数：将时间戳字符串转换为 RFC3339 格式
func timestampToRFC3339(ts string, tz string) string {
	if ts == "" {
		return ""
	}
	timestamp, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ts
	}

	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}

	return time.Unix(timestamp, 0).In(loc).Format(time.RFC3339)
}

// EventAttendee 日程参与人
type EventAttendee struct {
	Type            string `json:"type"`                        // user/chat/resource/third_party
	AttendeeID      string `json:"attendee_id,omitempty"`       // 参与人 ID
	UserID          string `json:"user_id,omitempty"`           // 用户 ID
	ChatID          string `json:"chat_id,omitempty"`           // 群 ID
	RoomID          string `json:"room_id,omitempty"`           // 会议室 ID
	ThirdPartyEmail string `json:"third_party_email,omitempty"` // 第三方邮箱
	DisplayName     string `json:"display_name,omitempty"`      // 显示名称
	RsvpStatus      string `json:"rsvp_status,omitempty"`       // 响应状态
	IsOptional      bool   `json:"is_optional,omitempty"`       // 是否可选参加
	IsOrganizer     bool   `json:"is_organizer,omitempty"`      // 是否组织者
	IsExternal      bool   `json:"is_external,omitempty"`       // 是否外部参与人
}

// FreebusyInfo 忙闲信息
type FreebusyInfo struct {
	StartTime string `json:"start_time"` // RFC3339
	EndTime   string `json:"end_time"`   // RFC3339
}

// GetCalendar 获取日历详情
func GetCalendar(calendarID string, userAccessToken string) (*Calendar, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}

	req := larkcalendar.NewGetCalendarReqBuilder().
		CalendarId(calendarID).
		Build()

	resp, err := client.Calendar.Calendar.Get(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, fmt.Errorf("获取日历详情失败: %w", err)
	}

	if !resp.Success() {
		return nil, fmt.Errorf("获取日历详情失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil {
		return nil, fmt.Errorf("日历不存在")
	}

	return &Calendar{
		CalendarID:   StringVal(resp.Data.CalendarId),
		Summary:      StringVal(resp.Data.Summary),
		Description:  StringVal(resp.Data.Description),
		Permissions:  StringVal(resp.Data.Permissions),
		Type:         StringVal(resp.Data.Type),
		Color:        IntVal(resp.Data.Color),
		Role:         StringVal(resp.Data.Role),
		SummaryAlias: StringVal(resp.Data.SummaryAlias),
		IsDeleted:    BoolVal(resp.Data.IsDeleted),
		IsThirdParty: BoolVal(resp.Data.IsThirdParty),
	}, nil
}

// GetPrimaryCalendar 获取主日历
func GetPrimaryCalendar(userAccessToken string) (*Calendar, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}

	req := larkcalendar.NewPrimaryCalendarReqBuilder().Build()

	resp, err := client.Calendar.Calendar.Primary(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, fmt.Errorf("获取主日历失败: %w", err)
	}

	if !resp.Success() {
		return nil, fmt.Errorf("获取主日历失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || len(resp.Data.Calendars) == 0 {
		return nil, fmt.Errorf("未找到主日历")
	}

	cal := resp.Data.Calendars[0].Calendar
	if cal == nil {
		return nil, fmt.Errorf("主日历数据为空")
	}

	return &Calendar{
		CalendarID:   StringVal(cal.CalendarId),
		Summary:      StringVal(cal.Summary),
		Description:  StringVal(cal.Description),
		Permissions:  StringVal(cal.Permissions),
		Type:         StringVal(cal.Type),
		Color:        IntVal(cal.Color),
		Role:         StringVal(cal.Role),
		SummaryAlias: StringVal(cal.SummaryAlias),
		IsDeleted:    BoolVal(cal.IsDeleted),
		IsThirdParty: BoolVal(cal.IsThirdParty),
	}, nil
}

// InstanceRelationInfo 日历事件实例的关联信息（会议实例 ID + 妙记 token）
type InstanceRelationInfo struct {
	MeetingInstanceIDs []string `json:"meeting_instance_ids,omitempty"`
	MeetingNotes       []string `json:"meeting_notes,omitempty"` // minute_tokens
}

// MgetInstanceRelationInfo 批量查询日历事件实例的会议/妙记关联信息
// API: POST /open-apis/calendar/v4/calendars/{calendar_id}/events/mget_instance_relation_info
// 返回 map[instance_id]InstanceRelationInfo
func MgetInstanceRelationInfo(calendarID string, instanceIDs []string, needNotes bool, userAccessToken string) (map[string]*InstanceRelationInfo, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"instance_ids":              instanceIDs,
		"need_meeting_instance_ids": true,
	}
	if needNotes {
		body["need_meeting_notes"] = true
	}

	tokenType, opts := resolveTokenOpts(userAccessToken)
	apiPath := fmt.Sprintf("/open-apis/calendar/v4/calendars/%s/events/mget_instance_relation_info", calendarID)

	resp, err := client.Post(Context(), apiPath, body, tokenType, opts...)
	if err != nil {
		return nil, fmt.Errorf("查询日历事件实例关联信息失败: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("查询日历事件实例关联信息失败: HTTP %d, body: %s", resp.StatusCode, string(resp.RawBody))
	}

	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			InstanceRelationInfos []struct {
				InstanceID         string   `json:"instance_id"`
				MeetingInstanceIDs []string `json:"meeting_instance_ids"`
				MeetingNotes       []string `json:"meeting_notes"`
			} `json:"instance_relation_infos"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.RawBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("查询日历事件实例关联信息失败: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
	}

	result := make(map[string]*InstanceRelationInfo, len(apiResp.Data.InstanceRelationInfos))
	for _, info := range apiResp.Data.InstanceRelationInfos {
		result[info.InstanceID] = &InstanceRelationInfo{
			MeetingInstanceIDs: info.MeetingInstanceIDs,
			MeetingNotes:       info.MeetingNotes,
		}
	}
	return result, nil
}

const (
	searchEventDefaultPageSize = 20
	searchEventMaxPageSize     = 30
)

// SearchEventsParams 是 current search_event 端点的请求参数。
type SearchEventsParams struct {
	CalendarID  string
	Query       string
	StartTime   string // RFC3339，写入 filter.time_range.start_time
	EndTime     string // RFC3339，写入 filter.time_range.end_time
	AttendeeIDs []string
	PageToken   string
	PageSize    int
}

type searchEventTimeRange struct {
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
}

type searchEventFilter struct {
	AttendeeUserIDs []string              `json:"attendee_user_ids,omitempty"`
	AttendeeChatIDs []string              `json:"attendee_chat_ids,omitempty"`
	MeetingRoomIDs  []string              `json:"meeting_room_ids,omitempty"`
	TimeRange       *searchEventTimeRange `json:"time_range,omitempty"`
}

type searchEventRequestBody struct {
	Query  string             `json:"query"`
	Filter *searchEventFilter `json:"filter,omitempty"`
}

func buildSearchEventFilter(startTime, endTime string, attendeeIDs []string) *searchEventFilter {
	var userIDs, chatIDs, roomIDs []string
	for _, id := range attendeeIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		switch {
		case strings.HasPrefix(id, "ou_"):
			userIDs = append(userIDs, id)
		case strings.HasPrefix(id, "oc_"):
			chatIDs = append(chatIDs, id)
		case strings.HasPrefix(id, "omm_"):
			roomIDs = append(roomIDs, id)
		default:
			userIDs = append(userIDs, id)
		}
	}
	var tr *searchEventTimeRange
	if startTime != "" || endTime != "" {
		tr = &searchEventTimeRange{StartTime: startTime, EndTime: endTime}
	}
	if len(userIDs) == 0 && len(chatIDs) == 0 && len(roomIDs) == 0 && tr == nil {
		return nil
	}
	return &searchEventFilter{
		AttendeeUserIDs: userIDs,
		AttendeeChatIDs: chatIDs,
		MeetingRoomIDs:  roomIDs,
		TimeRange:       tr,
	}
}

func searchEventTimeText(info *struct {
	Date     string `json:"date"`
	DateTime string `json:"date_time"`
	Timezone string `json:"timezone"`
}) string {
	if info == nil {
		return ""
	}
	if info.DateTime != "" {
		return info.DateTime
	}
	return info.Date
}

// SearchEvents 搜索日程（POST /calendars/{id}/events/search_event）。
func SearchEvents(calendarID, query string, startTime, endTime string, pageToken string, pageSize int, userAccessToken string) ([]*CalendarEvent, string, error) {
	return SearchEventsWithParams(SearchEventsParams{
		CalendarID: calendarID,
		Query:      query,
		StartTime:  startTime,
		EndTime:    endTime,
		PageToken:  pageToken,
		PageSize:   pageSize,
	}, userAccessToken)
}

// SearchEventsWithParams 按 current search_event 契约搜索日程。
func SearchEventsWithParams(params SearchEventsParams, userAccessToken string) ([]*CalendarEvent, string, error) {
	if strings.TrimSpace(params.CalendarID) == "" {
		params.CalendarID = "primary"
	}
	pageSize, err := ResolvePageSize(params.PageSize, searchEventDefaultPageSize, 1, searchEventMaxPageSize)
	if err != nil {
		return nil, "", fmt.Errorf("搜索日程失败: %w", err)
	}

	cli, err := GetClient()
	if err != nil {
		return nil, "", err
	}

	body := searchEventRequestBody{
		Query:  params.Query,
		Filter: buildSearchEventFilter(params.StartTime, params.EndTime, params.AttendeeIDs),
	}

	q := url.Values{}
	q.Set("page_size", strconv.Itoa(pageSize))
	if params.PageToken != "" {
		q.Set("page_token", params.PageToken)
	}
	apiPath := fmt.Sprintf("/open-apis/calendar/v4/calendars/%s/events/search_event?%s",
		url.PathEscape(params.CalendarID), q.Encode())

	tokenType, opts := resolveTokenOpts(userAccessToken)
	resp, err := cli.Post(Context(), apiPath, body, tokenType, opts...)
	if err != nil {
		return nil, "", fmt.Errorf("搜索日程失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("搜索日程失败: HTTP %d, body: %s", resp.StatusCode, string(resp.RawBody))
	}

	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				MetaData *struct {
					EventID  string `json:"event_id"`
					Summary  string `json:"summary"`
					AppLink  string `json:"app_link"`
					IsAllDay bool   `json:"is_all_day"`
					Start    *struct {
						Date     string `json:"date"`
						DateTime string `json:"date_time"`
						Timezone string `json:"timezone"`
					} `json:"start"`
					End *struct {
						Date     string `json:"date"`
						DateTime string `json:"date_time"`
						Timezone string `json:"timezone"`
					} `json:"end"`
				} `json:"meta_data"`
			} `json:"items"`
			PageToken string `json:"page_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.RawBody, &apiResp); err != nil {
		return nil, "", fmt.Errorf("解析搜索日程响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		return nil, "", fmt.Errorf("搜索日程失败: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
	}

	var events []*CalendarEvent
	for _, item := range apiResp.Data.Items {
		if item.MetaData == nil {
			continue
		}
		meta := item.MetaData
		ev := &CalendarEvent{
			EventID:   meta.EventID,
			Summary:   meta.Summary,
			AppLink:   meta.AppLink,
			StartTime: searchEventTimeText(meta.Start),
			EndTime:   searchEventTimeText(meta.End),
		}
		if meta.Start != nil && meta.Start.Timezone != "" {
			ev.TimeZone = meta.Start.Timezone
		}
		events = append(events, ev)
	}
	return events, apiResp.Data.PageToken, nil
}

// AddEventAttendees 添加日程参与人
func AddEventAttendees(calendarID, eventID string, attendees []*EventAttendee, userAccessToken string) error {
	client, err := GetClient()
	if err != nil {
		return err
	}

	var sdkAttendees []*larkcalendar.CalendarEventAttendee
	for _, a := range attendees {
		builder := larkcalendar.NewCalendarEventAttendeeBuilder().
			Type(a.Type)
		if a.UserID != "" {
			builder.UserId(a.UserID)
		}
		if a.ChatID != "" {
			builder.ChatId(a.ChatID)
		}
		if a.RoomID != "" {
			builder.RoomId(a.RoomID)
		}
		if a.ThirdPartyEmail != "" {
			builder.ThirdPartyEmail(a.ThirdPartyEmail)
		}
		sdkAttendees = append(sdkAttendees, builder.Build())
	}

	body := larkcalendar.NewCreateCalendarEventAttendeeReqBodyBuilder().
		Attendees(sdkAttendees).
		NeedNotification(true).
		Build()

	req := larkcalendar.NewCreateCalendarEventAttendeeReqBuilder().
		CalendarId(calendarID).
		EventId(eventID).
		Body(body).
		Build()

	resp, err := client.Calendar.CalendarEventAttendee.Create(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return fmt.Errorf("添加日程参与人失败: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("添加日程参与人失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	return nil
}

// ListEventAttendees 列出日程参与人
func ListEventAttendees(calendarID, eventID string, pageSize int, pageToken string, userAccessToken string) ([]*EventAttendee, string, bool, error) {
	client, err := GetClient()
	if err != nil {
		return nil, "", false, err
	}

	reqBuilder := larkcalendar.NewListCalendarEventAttendeeReqBuilder().
		CalendarId(calendarID).
		EventId(eventID)

	if pageSize > 0 {
		reqBuilder.PageSize(pageSize)
	}
	if pageToken != "" {
		reqBuilder.PageToken(pageToken)
	}

	resp, err := client.Calendar.CalendarEventAttendee.List(Context(), reqBuilder.Build(), UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, "", false, fmt.Errorf("获取日程参与人列表失败: %w", err)
	}

	if !resp.Success() {
		return nil, "", false, fmt.Errorf("获取日程参与人列表失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	var attendees []*EventAttendee
	if resp.Data != nil && resp.Data.Items != nil {
		for _, item := range resp.Data.Items {
			attendees = append(attendees, &EventAttendee{
				Type:            StringVal(item.Type),
				AttendeeID:      StringVal(item.AttendeeId),
				UserID:          StringVal(item.UserId),
				ChatID:          StringVal(item.ChatId),
				RoomID:          StringVal(item.RoomId),
				ThirdPartyEmail: StringVal(item.ThirdPartyEmail),
				DisplayName:     StringVal(item.DisplayName),
				RsvpStatus:      StringVal(item.RsvpStatus),
				IsOptional:      BoolVal(item.IsOptional),
				IsOrganizer:     BoolVal(item.IsOrganizer),
				IsExternal:      BoolVal(item.IsExternal),
			})
		}
	}

	var nextPageToken string
	var hasMore bool
	if resp.Data != nil {
		nextPageToken = StringVal(resp.Data.PageToken)
		hasMore = BoolVal(resp.Data.HasMore)
	}

	return attendees, nextPageToken, hasMore, nil
}

// ListFreebusy 查询忙闲信息
func ListFreebusy(startTime, endTime string, userID string, userAccessToken string) ([]*FreebusyInfo, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}

	bodyBuilder := larkcalendar.NewListFreebusyReqBodyBuilder().
		TimeMin(startTime).
		TimeMax(endTime)

	if userID != "" {
		bodyBuilder.UserId(userID)
	}

	req := larkcalendar.NewListFreebusyReqBuilder().
		Body(bodyBuilder.Build()).
		Build()

	resp, err := client.Calendar.Freebusy.List(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return nil, fmt.Errorf("查询忙闲信息失败: %w", err)
	}

	if !resp.Success() {
		return nil, fmt.Errorf("查询忙闲信息失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	var result []*FreebusyInfo
	if resp.Data != nil && resp.Data.FreebusyList != nil {
		for _, item := range resp.Data.FreebusyList {
			result = append(result, &FreebusyInfo{
				StartTime: StringVal(item.StartTime),
				EndTime:   StringVal(item.EndTime),
			})
		}
	}

	return result, nil
}

// ReplyEvent 回复日程（接受/拒绝/待定）
func ReplyEvent(calendarID, eventID, rsvpStatus string, userAccessToken string) error {
	client, err := GetClient()
	if err != nil {
		return err
	}

	body := larkcalendar.NewReplyCalendarEventReqBodyBuilder().
		RsvpStatus(rsvpStatus).
		Build()

	req := larkcalendar.NewReplyCalendarEventReqBuilder().
		CalendarId(calendarID).
		EventId(eventID).
		Body(body).
		Build()

	resp, err := client.Calendar.CalendarEvent.Reply(Context(), req, UserTokenOption(userAccessToken)...)
	if err != nil {
		return fmt.Errorf("回复日程失败: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("回复日程失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	return nil
}

// AgendaEvent 日程视图中的事件实例（展开重复日程后的独立实例）
type AgendaEvent struct {
	EventID        string `json:"event_id"`
	Summary        string `json:"summary"`
	StartTime      string `json:"start_time"`
	EndTime        string `json:"end_time"`
	Status         string `json:"status,omitempty"`
	FreeBusyStatus string `json:"free_busy_status,omitempty"`
	SelfRSVP       string `json:"self_rsvp_status,omitempty"`
	IsAllDay       bool   `json:"is_all_day,omitempty"`
}

const (
	maxInstanceViewSpanSeconds       = 40 * 24 * 60 * 60
	minSplitWindowSeconds            = 2 * 60 * 60
	maxInstanceViewSplitDepth        = 10
	larkErrCalendarTimeRangeExceeded = 193103 // instance_view 查询窗口超过 40 天
	larkErrCalendarTooManyInstances  = 193104 // instance_view 单窗口超过 1000 个实例
)

type agendaTimeInfo struct {
	Timestamp string `json:"timestamp"`
	Date      string `json:"date"`
	Timezone  string `json:"timezone"`
}

type agendaRawItem struct {
	EventID        string          `json:"event_id"`
	Summary        string          `json:"summary"`
	StartTime      *agendaTimeInfo `json:"start_time"`
	EndTime        *agendaTimeInfo `json:"end_time"`
	Status         string          `json:"status"`
	FreeBusyStatus string          `json:"free_busy_status"`
	SelfRSVP       string          `json:"self_rsvp_status"`
}

func instanceViewPath(calendarID string, startTime, endTime int64) string {
	q := url.Values{}
	q.Set("start_time", strconv.FormatInt(startTime, 10))
	q.Set("end_time", strconv.FormatInt(endTime, 10))
	return fmt.Sprintf("/open-apis/calendar/v4/calendars/%s/events/instance_view?%s",
		url.PathEscape(calendarID), q.Encode())
}

func fetchInstanceViewRange(calendarID string, startTime, endTime int64, depth int, userAccessToken string) ([]agendaRawItem, error) {
	if depth > maxInstanceViewSplitDepth {
		return nil, fmt.Errorf("获取日程视图失败: 拆分次数过多")
	}
	if startTime > endTime {
		return nil, nil
	}
	span := endTime - startTime
	if span > maxInstanceViewSpanSeconds {
		mid := startTime + span/2
		return fetchInstanceViewSplit(calendarID, startTime, mid, endTime, depth, userAccessToken)
	}

	cli, err := GetClient()
	if err != nil {
		return nil, err
	}
	tokenType, opts := resolveTokenOpts(userAccessToken)
	resp, err := cli.Get(Context(), instanceViewPath(calendarID, startTime, endTime), nil, tokenType, opts...)
	if err != nil {
		return nil, fmt.Errorf("获取日程视图失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取日程视图失败: HTTP %d, body: %s", resp.StatusCode, string(resp.RawBody))
	}

	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []agendaRawItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.RawBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		apiErr := fmt.Errorf("获取日程视图失败: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
		switch apiResp.Code {
		case larkErrCalendarTimeRangeExceeded:
			mid := startTime + span/2
			if mid <= startTime {
				return nil, apiErr
			}
			return fetchInstanceViewSplit(calendarID, startTime, mid, endTime, depth, userAccessToken)
		case larkErrCalendarTooManyInstances:
			if span <= minSplitWindowSeconds {
				return nil, apiErr
			}
			mid := startTime + span/2
			return fetchInstanceViewSplit(calendarID, startTime, mid, endTime, depth, userAccessToken)
		default:
			return nil, apiErr
		}
	}
	return apiResp.Data.Items, nil
}

func fetchInstanceViewSplit(calendarID string, startTime, mid, endTime int64, depth int, userAccessToken string) ([]agendaRawItem, error) {
	left, err := fetchInstanceViewRange(calendarID, startTime, mid, depth+1, userAccessToken)
	if err != nil {
		return nil, err
	}
	right, err := fetchInstanceViewRange(calendarID, mid+1, endTime, depth+1, userAccessToken)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func agendaTimeKey(info *agendaTimeInfo) string {
	if info == nil {
		return ""
	}
	if info.Timestamp != "" {
		return info.Timestamp
	}
	return info.Date
}

func agendaStartUnix(info *agendaTimeInfo) int64 {
	if info == nil {
		return 0
	}
	if info.Timestamp != "" {
		n, err := strconv.ParseInt(info.Timestamp, 10, 64)
		if err == nil {
			return n
		}
	}
	if info.Date != "" {
		if t, err := time.ParseInLocation("2006-01-02", info.Date, time.UTC); err == nil {
			return t.Unix()
		}
	}
	return 0
}

func exclusiveAllDayEndDate(dateStr string) string {
	t, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC)
	if err != nil {
		return dateStr
	}
	return t.Add(-1 * time.Second).Format("2006-01-02")
}

func convertAgendaRawItem(item agendaRawItem) *AgendaEvent {
	event := &AgendaEvent{
		EventID:        item.EventID,
		Summary:        item.Summary,
		Status:         item.Status,
		FreeBusyStatus: item.FreeBusyStatus,
		SelfRSVP:       item.SelfRSVP,
	}
	if item.StartTime != nil {
		tz := item.StartTime.Timezone
		if item.StartTime.Timestamp != "" {
			event.StartTime = timestampToRFC3339(item.StartTime.Timestamp, tz)
		} else if item.StartTime.Date != "" {
			event.StartTime = item.StartTime.Date
			event.IsAllDay = true
		}
	}
	if item.EndTime != nil {
		tz := item.EndTime.Timezone
		if item.EndTime.Timestamp != "" {
			event.EndTime = timestampToRFC3339(item.EndTime.Timestamp, tz)
		} else if item.EndTime.Date != "" {
			event.EndTime = exclusiveAllDayEndDate(item.EndTime.Date)
		}
	}
	return event
}

func dedupeAndSortAgendaItems(items []agendaRawItem) []agendaRawItem {
	seen := make(map[string]bool, len(items))
	out := make([]agendaRawItem, 0, len(items))
	for _, item := range items {
		key := item.EventID + "|" + agendaTimeKey(item.StartTime) + "|" + agendaTimeKey(item.EndTime)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return agendaStartUnix(out[i].StartTime) < agendaStartUnix(out[j].StartTime)
	})
	return out
}

// ListCalendarAgenda 获取日程实例视图（展开重复日程为独立实例）。
// instance_view 无服务端分页：pageSize/pageToken 被忽略，窗口超过 40 天或命中
// 193104 时由客户端拆分、去重；全天结束日按排他日期转为含当日。
func ListCalendarAgenda(calendarID string, startTime, endTime int64, pageSize int, pageToken string, userAccessToken string) ([]*AgendaEvent, string, bool, error) {
	_ = pageSize
	_ = pageToken
	if strings.TrimSpace(calendarID) == "" {
		calendarID = "primary"
	}

	items, err := fetchInstanceViewRange(calendarID, startTime, endTime, 0, userAccessToken)
	if err != nil {
		return nil, "", false, err
	}
	items = dedupeAndSortAgendaItems(items)

	events := make([]*AgendaEvent, 0, len(items))
	for _, item := range items {
		if item.Status == "cancelled" {
			continue
		}
		events = append(events, convertAgendaRawItem(item))
	}
	// instance_view 无伪分页：切分在客户端完成，对外始终一页。
	return events, "", false, nil
}

// 辅助函数：转换日程对象
func convertEvent(event *larkcalendar.CalendarEvent) *CalendarEvent {
	if event == nil {
		return nil
	}

	result := &CalendarEvent{
		EventID:     StringVal(event.EventId),
		OrganizerID: StringVal(event.OrganizerCalendarId),
		Summary:     StringVal(event.Summary),
		Description: StringVal(event.Description),
		Status:      StringVal(event.Status),
		Visibility:  StringVal(event.Visibility),
		RecurringID: StringVal(event.RecurringEventId),
		Recurrence:  StringVal(event.Recurrence),
		IsException: BoolVal(event.IsException),
		AppLink:     StringVal(event.AppLink),
		Color:       IntVal(event.Color),
	}

	// 时区
	tz := ""
	if event.StartTime != nil && event.StartTime.Timezone != nil {
		tz = *event.StartTime.Timezone
		result.TimeZone = tz
	}

	// 时间转换
	if event.StartTime != nil && event.StartTime.Timestamp != nil {
		result.StartTime = timestampToRFC3339(*event.StartTime.Timestamp, tz)
	}
	if event.EndTime != nil && event.EndTime.Timestamp != nil {
		result.EndTime = timestampToRFC3339(*event.EndTime.Timestamp, tz)
	}
	if event.Location != nil && event.Location.Name != nil {
		result.Location = *event.Location.Name
	}
	if event.CreateTime != nil {
		result.CreateTime = timestampToRFC3339(*event.CreateTime, tz)
	}

	return result
}
