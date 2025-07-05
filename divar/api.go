package divar

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type APIPath struct {
	SearchList string
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

var APIPaths = APIPath{
	SearchList: "/v8/postlist/w/search",
}

func Search(curlString string) (SearchRes, error) {
	if !strings.Contains(curlString, APIPaths.SearchList) {
		return SearchRes{}, errors.New("unsupported API endpoint")
	}

	/*
		In Divar’s `/v8/postlist/w/search` API, there’s a field called `last_post_date` that defines up to which date the API should return results.
		I believe this is mainly for caching purposes—if the frontend sends the same data across multiple requests,we can serve cached results.
		It also ensures that with multiple UI refreshes, the user sees consistent results instead of the latest updates. :)
		So, I’m going to increase it to `2030`, which is a far future date—this way, it will always show the newest changes.
	*/
	cmd := exec.Command("bash", "-c", strings.ReplaceAll(curlString, "2025-", "2030-"))
	output, err := cmd.Output()
	if err != nil {
		return SearchRes{}, err
	}

	var data SearchRes
	if err := json.Unmarshal(output, &data); err != nil {
		return SearchRes{}, err
	}
	return data, nil
}
