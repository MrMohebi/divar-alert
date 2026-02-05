package divar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type APIPath struct {
	SearchList string
	PostDetail string
}

type SearchRes struct {
	ListTopWidgets []struct {
		WidgetType string `json:"widget_type"`
		Data       struct {
			Type  string `json:"@type"`
			Title string `json:"title"`
			Icon  struct {
				ImageURLDark  string `json:"image_url_dark"`
				ImageURLLight string `json:"image_url_light"`
				IconName      string `json:"icon_name"`
				IconColor     string `json:"icon_color"`
			} `json:"icon"`
			Items []struct {
				Title  string `json:"title"`
				Action struct {
					Type    string `json:"type"`
					Payload struct {
						Type       string `json:"@type"`
						SearchData struct {
							FormData struct {
								Data struct {
									Category struct {
										Str struct {
											Value string `json:"value"`
										} `json:"str"`
									} `json:"category"`
									Sort struct {
										Str struct {
											Value string `json:"value"`
										} `json:"str"`
									} `json:"sort"`
									Price struct {
										NumberRange struct {
											Minimum string `json:"minimum"`
											Maximum string `json:"maximum"`
										} `json:"number_range"`
									} `json:"price"`
								} `json:"data"`
							} `json:"form_data"`
						} `json:"search_data"`
						SourceView    string `json:"source_view"`
						SourceViewStr string `json:"source_view_str"`
					} `json:"payload"`
				} `json:"action"`
			} `json:"items"`
		} `json:"data"`
	} `json:"list_top_widgets"`
	ListWidgets []struct {
		WidgetType string `json:"widget_type"`
		Data       struct {
			Type   string `json:"@type"`
			Title  string `json:"title"`
			Action struct {
				Type    string `json:"type"`
				Payload struct {
					Type    string `json:"@type"`
					Token   string `json:"token"`
					WebInfo struct {
						Title       string `json:"title"`
						CityPersian string `json:"city_persian"`
					} `json:"web_info"`
				} `json:"payload"`
			} `json:"action"`
			ImageURL              string `json:"image_url"`
			BottomDescriptionText string `json:"bottom_description_text"`
			RedText               string `json:"red_text"`
			MiddleDescriptionText string `json:"middle_description_text"`
			HasDivider            bool   `json:"has_divider"`
			ImageCount            int    `json:"image_count"`
			TopDescriptionText    string `json:"top_description_text"`
			ImageTopLeftTag       struct {
				Text string `json:"text"`
				Icon struct {
					ImageURLDark  string `json:"image_url_dark"`
					ImageURLLight string `json:"image_url_light"`
					IconName      string `json:"icon_name"`
					IconColor     string `json:"icon_color"`
				} `json:"icon"`
			} `json:"image_top_left_tag"`
			Token                    string `json:"token"`
			ShouldIndicateSeenStatus bool   `json:"should_indicate_seen_status"`
		} `json:"data"`
		ActionLog struct {
			ServerSideInfo struct {
				Info struct {
					Type       string `json:"@type"`
					PostToken  string `json:"post_token"`
					Index      int    `json:"index"`
					PostType   string `json:"post_type"`
					ListType   string `json:"list_type"`
					SourcePage string `json:"source_page"`
					ExtraData  struct {
						Type string `json:"@type"`
						Jli  struct {
							Sort struct {
								Value string `json:"value"`
							} `json:"sort"`
							Cities []string `json:"cities"`
							Price  struct {
								Max int `json:"max"`
								Min int `json:"min"`
							} `json:"price"`
							Category struct {
								Value string `json:"value"`
							} `json:"category"`
						} `json:"jli"`
						SearchUID  string `json:"search_uid"`
						SearchData struct {
							FormDataJSON      string   `json:"form_data_json"`
							ServerPayloadJSON string   `json:"server_payload_json"`
							Cities            []string `json:"cities"`
							QueryInputType    string   `json:"query_input_type"`
						} `json:"search_data"`
					} `json:"extra_data"`
					SortDate time.Time `json:"sort_date"`
				} `json:"info"`
				ItemType struct {
					Type string `json:"type"`
				} `json:"item_type"`
			} `json:"server_side_info"`
			Enabled bool `json:"enabled"`
		} `json:"action_log"`
	} `json:"list_widgets"`
	SearchData struct {
		FormData struct {
			Data struct {
				Category struct {
					Str struct {
						Value string `json:"value"`
					} `json:"str"`
				} `json:"category"`
				Price struct {
					NumberRange struct {
						Minimum string `json:"minimum"`
						Maximum string `json:"maximum"`
					} `json:"number_range"`
				} `json:"price"`
			} `json:"data"`
		} `json:"form_data"`
		ServerPayload struct {
			Type               string `json:"@type"`
			AdditionalFormData struct {
				Data struct {
					Sort struct {
						Str struct {
							Value string `json:"value"`
						} `json:"str"`
					} `json:"sort"`
				} `json:"data"`
			} `json:"additional_form_data"`
		} `json:"server_payload"`
	} `json:"search_data"`
	ActionLog struct {
		ServerSideInfo struct {
			Info struct {
				Type       string   `json:"@type"`
				Cities     []string `json:"cities"`
				CurrentTab string   `json:"current_tab"`
				SearchData struct {
					FormDataJSON      string   `json:"form_data_json"`
					ServerPayloadJSON string   `json:"server_payload_json"`
					Cities            []string `json:"cities"`
					QueryInputType    string   `json:"query_input_type"`
				} `json:"search_data"`
				Tokens      []string `json:"tokens"`
				HasNextPage bool     `json:"has_next_page"`
				Pelle       struct {
					Elastic struct {
						Tokens         []string `json:"tokens"`
						TotalHitsCount int      `json:"total_hits_count"`
						Documents      []struct {
							Token string `json:"token"`
							Type  string `json:"type"`
						} `json:"documents"`
					} `json:"elastic"`
				} `json:"pelle"`
				LastPostDateEpoch string `json:"last_post_date_epoch"`
				SearchID          string `json:"search_id"`
				SearchUID         string `json:"search_uid"`
				SourceView        string `json:"source_view"`
				PostsMetadata     []struct {
					Token    string `json:"token"`
					SortDate string `json:"sort_date"`
					Source   string `json:"source"`
				} `json:"posts_metadata"`
				Jli struct {
					Sort struct {
						Value string `json:"value"`
					} `json:"sort"`
					Cities []string `json:"cities"`
					Price  struct {
						Min int `json:"min"`
						Max int `json:"max"`
					} `json:"price"`
					Category struct {
						Value string `json:"value"`
					} `json:"category"`
				} `json:"jli"`
				SearchLayer string `json:"search_layer"`
				AdsBanner   struct {
					AuctionIds []string `json:"auction_ids"`
					Ads        []struct {
						AdInstanceID string `json:"ad_instance_id"`
						Index        int    `json:"index"`
						Offset       int    `json:"offset"`
					} `json:"ads"`
				} `json:"ads_banner"`
			} `json:"info"`
			ItemType struct {
				Type string `json:"type"`
			} `json:"item_type"`
		} `json:"server_side_info"`
		Enabled bool `json:"enabled"`
	} `json:"action_log"`
	SearchBar struct {
		Bookmark struct {
			ToggleActionLog struct {
				ServerSideInfo struct {
					ItemType struct {
						Type string `json:"type"`
					} `json:"item_type"`
				} `json:"server_side_info"`
				Enabled bool `json:"enabled"`
			} `json:"toggle_action_log"`
			Enabled bool `json:"enabled"`
		} `json:"bookmark"`
	} `json:"search_bar"`
	Pagination struct {
		HasNextPage bool `json:"has_next_page"`
		Data        struct {
			Type                   string    `json:"@type"`
			LastPostDate           time.Time `json:"last_post_date"`
			Page                   int       `json:"page"`
			LayerPage              int       `json:"layer_page"`
			SearchUID              string    `json:"search_uid"`
			CumulativeWidgetsCount int       `json:"cumulative_widgets_count"`
		} `json:"data"`
		IsFirstPage bool `json:"is_first_page"`
	} `json:"pagination"`
	SearchID   string `json:"search_id"`
	SeoDetails struct {
		Title          string `json:"title"`
		Description    string `json:"description"`
		Headline       string `json:"headline"`
		RobotsMetadata struct {
			Follow bool `json:"follow"`
		} `json:"robots_metadata"`
		BreadCrumb []struct {
			Name       string `json:"name"`
			SearchData struct {
				FormData struct {
					Data struct {
						Category struct {
							Str struct {
								Value string `json:"value"`
							} `json:"str"`
						} `json:"category"`
					} `json:"data"`
				} `json:"form_data"`
			} `json:"search_data,omitempty"`
		} `json:"bread_crumb"`
	} `json:"seo_details"`
}

// PostDetailRes represents the response structure for a single post detail
type PostDetailRes struct {
	Sections  []Section `json:"sections"`
	Share     Share     `json:"share"`
	Seo       Seo       `json:"seo"`
	Contact   Contact   `json:"contact"`
	Webengage Webengage `json:"webengage"`
	Analytics Analytics `json:"analytics"`
	City      City      `json:"city"`
}

type City struct {
	CityID     string `json:"city_id"`
	Name       string `json:"name"`
	ParentID   string `json:"parent_id"`
	IsProvince bool   `json:"is_province"`
	SecondSlug string `json:"second_slug"`
}

type Analytics struct {
	Cat1 string `json:"cat1"`
	Cat2 string `json:"cat2"`
	Cat3 string `json:"cat3"`
	City string `json:"city"`
}

type Webengage struct {
	Originality  string `json:"originality"`
	City         string `json:"city"`
	Cat3         string `json:"cat_3"`
	BusinessRef  string `json:"business_ref"`
	Token        string `json:"token"`
	ImageCount   int    `json:"image_count"`
	Category     string `json:"category"`
	Rent         int    `json:"rent"`
	SourceView   string `json:"source_view"`
	BusinessType string `json:"business_type"`
	Price        int    `json:"price"`
	Credit       int    `json:"credit"`
	District     string `json:"district"`
	BrandModel   string `json:"brand_model"`
	Gender       string `json:"gender"`
	Cat2         string `json:"cat_2"`
	Status       string `json:"status"`
	Cat1         string `json:"cat_1"`
}

type Contact struct {
	ChatEnabled                  bool       `json:"chat_enabled"`
	ApplyEnabled                 bool       `json:"apply_enabled"`
	ContactEncryptedData         string     `json:"contact_encrypted_data"`
	SecureCallEnabled            bool       `json:"secure_call_enabled"`
	ExperimentalAPIEnabled       bool       `json:"experimental_api_enabled"`
	ActionLog                    ActionLog2 `json:"action_log"`
	ContactUUID                  string     `json:"contact_uuid"`
	TrackerSessionID             string     `json:"tracker_session_id"`
	SupplierChatAssistantEnabled bool       `json:"supplier_chat_assistant_enabled"`
	EshopEnabled                 bool       `json:"eshop_enabled"`
}

type ActionLog2 struct {
	ServerSideInfo ServerSideInfo2 `json:"server_side_info"`
	Enabled        bool            `json:"enabled"`
}

type ServerSideInfo2 struct {
	Info     Info     `json:"info"`
	ItemType ItemType `json:"item_type"`
}

type Info struct {
	Type                     string `json:"@type"`
	PostToken                string `json:"post_token"`
	SourceView               string `json:"source_view"`
	EventID                  string `json:"event_id"`
	MethodName               string `json:"method_name"`
	ExperimentalAPIEnabled   bool   `json:"experimental_api_enabled"`
	ContactUUID              string `json:"contact_uuid"`
	ContactLayerExperimentID int    `json:"contact_layer_experiment_id"`
	TrackerSessionID         string `json:"tracker_session_id"`
	CallMethod               string `json:"call_method"`
}

type Seo struct {
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	AndroidPackageName string          `json:"android_package_name"`
	AndroidAppURL      string          `json:"android_app_url"`
	WebInfo            WebInfo         `json:"web_info"`
	UnavailableAfter   string          `json:"unavailable_after"`
	BreadCrumb         json.RawMessage `json:"bread_crumb"`
}

type WebInfo struct {
	Title               string `json:"title"`
	DistrictPersian     string `json:"district_persian"`
	CityPersian         string `json:"city_persian"`
	CategorySlugPersian string `json:"category_slug_persian"`
}

type Share struct {
	Title    string `json:"title"`
	WebURL   string `json:"web_url"`
	ImageURL string `json:"image_url"`
}

type Section struct {
	SectionName string   `json:"section_name"`
	Widgets     []Widget `json:"widgets"`
}

type Widget struct {
	WidgetType          string     `json:"widget_type"`
	Data                WidgetData `json:"data"`
	VisibilityCondition []string   `json:"visibility_condition"`
	UID                 string     `json:"uid"`
	ActionLog           *ActionLog `json:"action_log,omitempty"`
}

type ActionLog struct {
	ServerSideInfo ServerSideInfo `json:"server_side_info"`
	Enabled        bool           `json:"enabled"`
}

type ServerSideInfo struct {
	Info     AdditionalData `json:"info"`
	ItemType ItemType       `json:"item_type"`
}

type ItemType struct {
	Type string `json:"type"`
}

type WidgetData struct {
	Type                 string            `json:"@type"`
	ParentItems          []ParentItem      `json:"parent_items,omitempty"`
	CurrentPageTitle     string            `json:"current_page_title,omitempty"`
	Padded               bool              `json:"padded,omitempty"`
	Title                string            `json:"title,omitempty"`
	Subtitle             string            `json:"subtitle,omitempty"`
	HasDivider           bool              `json:"has_divider,omitempty"`
	ImageURL             string            `json:"image_url,omitempty"`
	Tags                 []string          `json:"tags,omitempty"`
	ShowThumbnail        bool              `json:"show_thumbnail,omitempty"`
	MobileDesign         bool              `json:"mobile_design,omitempty"`
	HighLevelHeading     bool              `json:"high_level_heading,omitempty"`
	Action               *Action2          `json:"action,omitempty"`
	HasNotification      bool              `json:"has_notification,omitempty"`
	Icon                 *Icon             `json:"icon,omitempty"`
	NotificationText     string            `json:"notification_text,omitempty"`
	Description          string            `json:"description,omitempty"`
	HasArrow             bool              `json:"has_arrow,omitempty"`
	Small                bool              `json:"small,omitempty"`
	LastNotificationDate string            `json:"last_notification_date,omitempty"`
	UID                  string            `json:"uid,omitempty"`
	Fullwidth            bool              `json:"fullwidth,omitempty"`
	Text                 string            `json:"text,omitempty"`
	Color                string            `json:"color,omitempty"`
	TextColor            string            `json:"text_color,omitempty"`
	DescriptionColor     string            `json:"description_color,omitempty"`
	SubtitleColor        string            `json:"subtitle_color,omitempty"`
	TextType             string            `json:"text_type,omitempty"`
	ShowRequired         bool              `json:"show_required,omitempty"`
	State                string            `json:"state,omitempty"`
	IsStickyHeader       bool              `json:"is_sticky_header,omitempty"`
	IsPrimary            bool              `json:"is_primary,omitempty"`
	Expandable           bool              `json:"expandable,omitempty"`
	PreviewMaxLine       int               `json:"preview_max_line,omitempty"`
	Items                []Item            `json:"items,omitempty"`
	ImageAspectRatio     *ImageAspectRatio `json:"image_aspect_ratio,omitempty"`
	HasPreview           bool              `json:"has_preview,omitempty"`
	HasThumbnails        bool              `json:"has_thumbnails,omitempty"`
	ScaleType            string            `json:"scale_type,omitempty"`
	ShowTooltip          bool              `json:"show_tooltip,omitempty"`
	TooltipData          *TooltipData      `json:"tooltip_data,omitempty"`
	Value                string            `json:"value,omitempty"`
	Compact              bool              `json:"compact,omitempty"`
	HasCopyToClipboard   bool              `json:"has_copy_to_clipboard,omitempty"`
	SendActionLogOnCopy  bool              `json:"send_action_log_on_copy,omitempty"`
	ImageColor           string            `json:"image_color,omitempty"`
	Disabled             bool              `json:"disabled,omitempty"`
	Credit               *Credit           `json:"credit,omitempty"`
	Rent                 *Credit           `json:"rent,omitempty"`
	Label                string            `json:"label,omitempty"`
	ActionTitle          string            `json:"action_title,omitempty"`
	TitleColor           string            `json:"title_color,omitempty"`
	Padding              string            `json:"padding,omitempty"`
	ActionText           string            `json:"action_text,omitempty"`
	ChipList             *ChipList         `json:"chip_list,omitempty"`
	Location             *Location         `json:"location,omitempty"`
	PreviewType          string            `json:"preview_type,omitempty"`
	ButtonTitle          string            `json:"button_title,omitempty"`
	PostToken            string            `json:"post_token,omitempty"`
}

type Location struct {
	Type      string     `json:"type"`
	ExactData *ExactData `json:"exact_data"`
}

type ExactData struct {
	Point Point `json:"point"`
}

type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type ChipList struct {
	Chips []Chip `json:"chips"`
}

type Chip struct {
	Text    string  `json:"text"`
	Type    string  `json:"type"`
	Style   string  `json:"style"`
	Rounded bool    `json:"rounded"`
	Small   bool    `json:"small"`
	Action  *Action `json:"action"`
}

type Credit struct {
	Value            string `json:"value"`
	TransformedValue string `json:"transformed_value"`
}

type TooltipData struct {
	Icon *Icon  `json:"icon"`
	Text string `json:"text"`
}

type ImageAspectRatio struct {
	DynamicAspectRatio DynamicAspectRatio `json:"dynamic_aspect_ratio"`
}

type DynamicAspectRatio struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

type Item struct {
	Title     string `json:"title"`
	Value     string `json:"value,omitempty"`
	Icon      *Icon  `json:"icon,omitempty"`
	Available bool   `json:"available,omitempty"`
}

type Icon struct {
	ImageURLDark  string `json:"image_url_dark"`
	ImageURLLight string `json:"image_url_light"`
	IconName      string `json:"icon_name"`
	IconColor     string `json:"icon_color"`
}

type Action2 struct {
	Type        string   `json:"type"`
	Payload     Payload2 `json:"payload"`
	PagePopLink bool     `json:"page_pop_link"`
}

type Payload2 struct {
	Type            string         `json:"@type"`
	AdditionalData  AdditionalData `json:"additional_data"`
	Page            string         `json:"page"`
	IsModal         bool           `json:"is_modal"`
	RestRequestPath string         `json:"rest_request_path"`
	GrpcRequestPath string         `json:"grpc_request_path"`
}

type AdditionalData struct {
	Type      string `json:"@type"`
	PostToken string `json:"post_token"`
}

type ParentItem struct {
	Title  string  `json:"title"`
	Action *Action `json:"action"`
}

type Action struct {
	Type        string  `json:"type"`
	Payload     Payload `json:"payload"`
	PagePopLink bool    `json:"page_pop_link"`
}

type Payload struct {
	Type          string     `json:"@type"`
	SearchData    SearchData `json:"search_data"`
	SourceView    string     `json:"source_view"`
	Tooltip       string     `json:"tooltip"`
	Refresh       bool       `json:"refresh"`
	SourceViewStr string     `json:"source_view_str"`
	IsNotLink     bool       `json:"is_not_link"`
	Referer       string     `json:"referer"`
}

type SearchData struct {
	FormData                  FormData `json:"form_data"`
	Query                     string   `json:"query"`
	QueryInputType            string   `json:"query_input_type"`
	OnlineRequestResponseData struct{} `json:"online_request_response_data"`
}

type FormData struct {
	Data FormDataContent `json:"data"`
}

type FormDataContent struct {
	Category CategoryData `json:"category"`
}

type CategoryData struct {
	Str StringValue `json:"str"`
}

type StringValue struct {
	Value string `json:"value"`
}

var APIPaths = APIPath{
	SearchList: "/v8/postlist/w/search",
	PostDetail: "/v8/posts-v2/web",
}

// parseCurlCommand extracts URL, headers, and body from a curl command string
func parseCurlCommand(curlString string) (url string, headers map[string]string, body string, err error) {
	headers = make(map[string]string)

	// Normalize the curl command: remove line continuations and extra whitespace
	curlString = strings.ReplaceAll(curlString, "\\\n", " ")
	curlString = strings.ReplaceAll(curlString, "\\\r\n", " ")
	curlString = regexp.MustCompile(`\s+`).ReplaceAllString(curlString, " ")
	curlString = strings.TrimSpace(curlString)

	// Extract URL - look for URL after 'curl' and before any flags
	urlRegex := regexp.MustCompile(`curl\s+['"]?([^'"\s-]+)['"]?`)
	urlMatches := urlRegex.FindStringSubmatch(curlString)
	if len(urlMatches) < 2 {
		return "", nil, "", errors.New("failed to extract URL from curl command")
	}
	url = urlMatches[1]

	// Extract headers (-H or --header)
	// This regex handles headers more robustly by capturing everything between quotes
	headerRegex := regexp.MustCompile(`(?:-H|--header)\s+['"]([^'"]+)['"]`)
	headerMatches := headerRegex.FindAllStringSubmatch(curlString, -1)
	for _, match := range headerMatches {
		if len(match) >= 2 {
			// Split on first colon to separate header name and value
			headerContent := match[1]
			colonIdx := strings.Index(headerContent, ":")
			if colonIdx > 0 {
				headerName := strings.TrimSpace(headerContent[:colonIdx])
				headerValue := strings.TrimSpace(headerContent[colonIdx+1:])
				headers[headerName] = headerValue
			}
		}
	}

	// Extract body data - handle both single and double quotes
	// Try to find data flags and extract the quoted content that follows
	dataPatterns := []string{
		`--data-raw\s+'([^']*(?:''[^']*)*)'`, // Single quotes (handle escaped quotes)
		`--data-raw\s+"([^"]*(?:""[^"]*)*)"`, // Double quotes (handle escaped quotes)
		`--data\s+'([^']*(?:''[^']*)*)'`,     // --data with single quotes
		`--data\s+"([^"]*(?:""[^"]*)*)"`,     // --data with double quotes
		`-d\s+'([^']*(?:''[^']*)*)'`,         // -d with single quotes
		`-d\s+"([^"]*(?:""[^']*)*)"`,         // -d with double quotes
	}

	for _, pattern := range dataPatterns {
		dataRegex := regexp.MustCompile(pattern)
		dataMatches := dataRegex.FindStringSubmatch(curlString)
		if len(dataMatches) >= 2 {
			body = dataMatches[1]
			// Unescape doubled quotes if present
			body = strings.ReplaceAll(body, "''", "'")
			body = strings.ReplaceAll(body, `""`, `"`)
			break
		}
	}

	return url, headers, body, nil
}

func Search(curlString string) (SearchRes, error) {
	if !strings.Contains(curlString, APIPaths.SearchList) {
		return SearchRes{}, errors.New("unsupported API endpoint")
	}

	// Parse curl command instead of executing it
	url, headers, body, err := parseCurlCommand(curlString)
	if err != nil {
		return SearchRes{}, fmt.Errorf("failed to parse curl command: %w", err)
	}

	/*
		In Divar's `/v8/postlist/w/search` API, there's a field called `last_post_date` that defines up to which date the API should return results.
		I believe this is mainly for caching purposes—if the frontend sends the same data across multiple requests,we can serve cached results.
		It also ensures that with multiple UI refreshes, the user sees consistent results instead of the latest updates. :)
		So, I'm going to increase it to `2030`, which is a far future date—this way, it will always show the newest changes.
	*/
	body = strings.ReplaceAll(body, "2026-", "2030-")

	// Create HTTP request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	var req *http.Request
	if body != "" {
		req, err = http.NewRequest("POST", url, bytes.NewBufferString(body))
	} else {
		req, err = http.NewRequest("GET", url, nil)
	}
	if err != nil {
		return SearchRes{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return SearchRes{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SearchRes{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read response
	output, err := io.ReadAll(resp.Body)
	if err != nil {
		return SearchRes{}, fmt.Errorf("failed to read response: %w", err)
	}

	var data SearchRes
	if err := json.Unmarshal(output, &data); err != nil {
		return SearchRes{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return data, nil
}

// GetPost fetches a single post detail by its token
// Note: This returns the raw API response. The structure varies by listing type (rental, sale, etc.)
func GetPost(postToken string) (PostDetailRes, error) {
	postToken = strings.TrimSpace(postToken)
	if postToken == "" {
		return PostDetailRes{}, errors.New("post token is required")
	}

	// Set default headers
	headers := map[string]string{
		"accept":          "application/json-filled",
		"accept-language": "en-US,en;q=0.9",
	}

	// Construct URL
	url := fmt.Sprintf("https://api.divar.ir%s/%s", APIPaths.PostDetail, postToken)

	// Create HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return PostDetailRes{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read response
	output, err := io.ReadAll(resp.Body)
	if err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to read response: %w", err)
	}

	var data PostDetailRes
	if err := json.Unmarshal(output, &data); err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return data, nil
}

// RentalPostImportantData holds the most important extracted information from a rental post (apartment/house)
// Note: This struct is specific to rental listings. Other listing types (cars, electronics, etc.) will have different fields.
type RentalPostImportantData struct {
	Token           string                `json:"token"`
	Title           string                `json:"title"`
	Subtitle        string                `json:"subtitle"`
	Description     string                `json:"description"`
	Price           RentalPriceInfo       `json:"price"`
	PropertyDetails RentalPropertyDetails `json:"details"`
	Location        LocationInfo          `json:"location"`
	Images          []string              `json:"images"`
	Features        []FeatureInfo         `json:"features"`
	ContactInfo     ContactDetails        `json:"contact"`
	City            string                `json:"city"`
	CityName        string                `json:"city_name"`
	Category        string                `json:"category"`
	PostedDate      string                `json:"posted_date"`
	ExpiresAt       string                `json:"expires_at"`
	URL             string                `json:"url"`
}

// RentalPriceInfo contains pricing information for rental listings (deposit + monthly rent)
type RentalPriceInfo struct {
	Deposit      string `json:"deposit"`
	Rent         string `json:"rent"`
	TransDeposit string `json:"trans_deposit"`
	TransRent    string `json:"trans_rent"`
}

// RentalPropertyDetails contains property-specific details for rental listings
type RentalPropertyDetails struct {
	Area      string `json:"area"`
	BuildYear string `json:"build_year"`
	Rooms     string `json:"rooms"`
	Floor     string `json:"floor"`
}

type LocationInfo struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
	MapImage  string  `json:"map_image"`
}

type FeatureInfo struct {
	Title     string `json:"title"`
	Available bool   `json:"available"`
}

type ContactDetails struct {
	ChatEnabled       bool   `json:"chat_enabled"`
	ContactUUID       string `json:"contact_uuid"`
	SecureCallEnabled bool   `json:"secure_call_enabled"`
}

// ExtractRentalImportantData extracts and structures the most important data from a rental post (apartment/house)
// This function is specifically designed for rental listings. Other listing types will require different extraction logic.
func ExtractRentalImportantData(post PostDetailRes) RentalPostImportantData {
	data := RentalPostImportantData{
		Token:     post.Webengage.Token,
		City:      post.Webengage.City,
		CityName:  post.City.Name,
		Category:  post.Webengage.Category,
		URL:       post.Share.WebURL,
		ExpiresAt: post.Seo.UnavailableAfter,
		ContactInfo: ContactDetails{
			ChatEnabled:       post.Contact.ChatEnabled,
			ContactUUID:       post.Contact.ContactUUID,
			SecureCallEnabled: post.Contact.SecureCallEnabled,
		},
	}

	// Extract data from sections
	for _, section := range post.Sections {
		switch section.SectionName {
		case "BREADCRUMB", "POST_TITLE":
			// Extract title and subtitle from first widget
			if len(section.Widgets) > 0 {
				widget := section.Widgets[0]
				if data.Title == "" && widget.Data.Title != "" {
					data.Title = widget.Data.Title
				}
				if data.Subtitle == "" && widget.Data.Subtitle != "" {
					data.Subtitle = widget.Data.Subtitle
				}
			}

		case "DESCRIPTION":
			// Extract description
			for _, widget := range section.Widgets {
				if widget.WidgetType == "DESCRIPTION_ROW" && widget.Data.Text != "" {
					data.Description = widget.Data.Text
					break
				}
			}

		case "IMAGE":
			// Extract images
			for _, widget := range section.Widgets {
				if widget.WidgetType == "IMAGE_CAROUSEL" && len(widget.Data.Items) > 0 {
					for _, item := range widget.Data.Items {
						if item.Icon != nil && item.Icon.ImageURLLight != "" {
							data.Images = append(data.Images, item.Icon.ImageURLLight)
						}
					}
					break
				}
			}

		case "LIST_DATA":
			// Extract property details, price, floor, and features
			for i, widget := range section.Widgets {
				switch widget.WidgetType {
				case "GROUP_INFO_ROW":
					// Property details (area, build year, rooms)
					if len(widget.Data.Items) >= 3 {
						if widget.Data.Items[0].Title == "متراژ" {
							data.PropertyDetails.Area = widget.Data.Items[0].Value
						}
						if widget.Data.Items[1].Title == "ساخت" {
							data.PropertyDetails.BuildYear = widget.Data.Items[1].Value
						}
						if widget.Data.Items[2].Title == "اتاق" {
							data.PropertyDetails.Rooms = widget.Data.Items[2].Value
						}
					}

				case "UNEXPANDABLE_ROW":
					// Check for price information
					if widget.Data.Credit != nil {
						data.Price.Deposit = widget.Data.Credit.Value
						data.Price.TransDeposit = widget.Data.Credit.TransformedValue
					}
					if widget.Data.Rent != nil {
						data.Price.Rent = widget.Data.Rent.Value
						data.Price.TransRent = widget.Data.Rent.TransformedValue
					}

					// Check for floor information
					if widget.Data.Title == "طبقه" && widget.Data.Value != "" {
						data.PropertyDetails.Floor = widget.Data.Value
					}

				case "GROUP_FEATURE_ROW":
					// Extract features
					if len(widget.Data.Items) > 0 {
						for _, item := range widget.Data.Items {
							data.Features = append(data.Features, FeatureInfo{
								Title:     item.Title,
								Available: item.Available,
							})
						}
					}
				}

				// Extract posted date from subtitle if available in early widgets
				if i == 0 && data.PostedDate == "" && widget.Data.Subtitle != "" {
					data.PostedDate = widget.Data.Subtitle
				}
			}

		case "MAP":
			// Extract location
			for _, widget := range section.Widgets {
				if widget.WidgetType == "MAP_ROW" && widget.Data.Location != nil {
					if widget.Data.Location.ExactData != nil {
						data.Location.Latitude = widget.Data.Location.ExactData.Point.Latitude
						data.Location.Longitude = widget.Data.Location.ExactData.Point.Longitude
					}
					if widget.Data.ImageURL != "" {
						data.Location.MapImage = widget.Data.ImageURL
					}
					break
				}
			}
		}
	}

	return data
}
