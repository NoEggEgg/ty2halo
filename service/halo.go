package service

import (
	consolesdk "api.halo.run/apis/openapi-go-console"
	extensionsdk "api.halo.run/apis/openapi-go-extension"
	publicsdk "api.halo.run/apis/openapi-go-public"
	usersdk "api.halo.run/apis/openapi-go-user"
	"bytes"
	"context"
	"github.com/fghwett/typecho-to-halo/config"
	"github.com/google/uuid"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	apiVersion = `content.halo.run/v1alpha1`

	kindTag        = "Tag"
	kindPost       = "Post"
	kindCategory   = "Category"
	kindSinglePage = "SinglePage"

	contentRawType = "markdown"
	contentVersion = 1

	commentGroup   = "content.halo.run"
	commentVersion = "v1alpha1"

	visiblePublic   = "PUBLIC"
	visibleInternal = "INTERNAL"
	visiblePrivate  = "PRIVATE"
)

type Halo struct {
	conf *config.Halo

	//aggregated *aggregatedsdk.APIClient
	console   *consolesdk.APIClient
	extension *extensionsdk.APIClient
	public    *publicsdk.APIClient
	user      *usersdk.APIClient
}

func NewHalo(conf *config.Halo) *Halo {
	//aggregatedConf := aggregatedsdk.NewConfiguration()
	//aggregatedConf.Host = conf.Host
	//aggregatedConf.Scheme = conf.Schema
	//aggregatedConf.Debug = conf.Debug
	//aggregatedConf.AddDefaultHeader("Authorization", "Bearer "+conf.Token)
	//aggregatedClient := aggregatedsdk.NewAPIClient(aggregatedConf)

	consoleConf := consolesdk.NewConfiguration()
	consoleConf.Host = conf.Host
	consoleConf.Scheme = conf.Schema
	consoleConf.Debug = conf.Debug
	consoleConf.AddDefaultHeader("Authorization", "Bearer "+conf.Token)
	consoleClient := consolesdk.NewAPIClient(consoleConf)

	extensionConf := extensionsdk.NewConfiguration()
	extensionConf.Host = conf.Host
	extensionConf.Scheme = conf.Schema
	extensionConf.Debug = conf.Debug
	extensionConf.AddDefaultHeader("Authorization", "Bearer "+conf.Token)
	extensionClient := extensionsdk.NewAPIClient(extensionConf)

	publicConf := publicsdk.NewConfiguration()
	publicConf.Host = conf.Host
	publicConf.Scheme = conf.Schema
	publicConf.Debug = conf.Debug
	publicConf.AddDefaultHeader("Authorization", "Bearer "+conf.Token)
	publicClient := publicsdk.NewAPIClient(publicConf)

	userConf := usersdk.NewConfiguration()
	userConf.Host = conf.Host
	userConf.Scheme = conf.Schema
	userConf.Debug = conf.Debug
	userConf.AddDefaultHeader("Authorization", "Bearer "+conf.Token)
	userClient := usersdk.NewAPIClient(userConf)

	h := &Halo{
		conf: conf,
		//aggregated: aggregated,
		console:   consoleClient,
		extension: extensionClient,
		public:    publicClient,
		user:      userClient,
	}

	return h
}

func (halo *Halo) GetTags() ([]extensionsdk.Tag, error) {
	result, _, err := halo.extension.TagV1alpha1API.ListTag(context.Background()).Page(1).Size(0).Execute()
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (halo *Halo) GetCategories() ([]extensionsdk.Category, error) {
	result, _, err := halo.extension.CategoryV1alpha1API.ListCategory(context.Background()).Page(1).Size(0).Execute()
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (halo *Halo) GetPosts() ([]consolesdk.ListedPost, error) {
	result, _, err := halo.console.PostV1alpha1ConsoleAPI.ListPosts(context.Background()).Page(1).Size(0).Execute()
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (halo *Halo) GetPages() ([]consolesdk.ListedSinglePage, error) {
	result, _, err := halo.console.SinglePageV1alpha1ConsoleAPI.ListSinglePages(context.Background()).Page(1).Size(0).Execute()
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (halo *Halo) AddTag(name, slug string) (*extensionsdk.Tag, error) {
	result, _, err := halo.extension.TagV1alpha1API.CreateTag(context.Background()).Tag(extensionsdk.Tag{
		ApiVersion: apiVersion,
		Kind:       kindTag,
		Metadata: extensionsdk.Metadata{
			Name: uuid.New().String(),
		},
		Spec: extensionsdk.TagSpec{
			DisplayName: name,
			Slug:        slug,
		},
	}).Execute()

	return result, err
}

func (halo *Halo) AddCategory(name, slug, description string, priority int32) (*extensionsdk.Category, error) {
	result, _, err := halo.extension.CategoryV1alpha1API.CreateCategory(context.Background()).Category(extensionsdk.Category{
		ApiVersion: apiVersion,
		Kind:       kindCategory,
		Metadata: extensionsdk.Metadata{
			Name: uuid.New().String(),
		},
		Spec: extensionsdk.CategorySpec{
			Children:                      []string{},
			Cover:                         extensionsdk.PtrString(""),
			Description:                   extensionsdk.PtrString(description),
			HideFromList:                  extensionsdk.PtrBool(false),
			PostTemplate:                  extensionsdk.PtrString(""),
			PreventParentPostCascadeQuery: extensionsdk.PtrBool(false),
			DisplayName:                   name,
			Priority:                      priority,
			Slug:                          slug,
			Template:                      extensionsdk.PtrString(""),
		},
	}).Execute()

	return result, err
}

func (halo *Halo) AddCategoryChildren(name string, children []string) (*extensionsdk.Category, error) {
	result, _, err := halo.extension.CategoryV1alpha1API.PatchCategory(context.Background(), name).JsonPatchInner([]extensionsdk.JsonPatchInner{
		{
			AddOperation: &extensionsdk.AddOperation{
				Op:    "add",
				Path:  "/spec/children",
				Value: children,
			},
		},
	}).Execute()
	//result, _, err := halo.extension.CategoryV1alpha1API.UpdateCategory(context.Background(), category.Metadata.Name).Category(*category).Execute()
	return result, err
}

func (halo *Halo) AddAttachment(filename string) (*consolesdk.Attachment, error) {
	f, e := os.Open(filename)
	if e != nil {
		return nil, e
	}

	attachment, resp, err := halo.console.AttachmentV1alpha1ConsoleAPI.UploadAttachment(context.Background()).PolicyName(halo.conf.PolicyName).GroupName(halo.conf.GroupName).File(f).Execute()
	if err == nil {
		defer func() {
			if er := resp.Body.Close(); err != nil {
				slog.Error("关闭上传文件失败", slog.Any("err", er), slog.String("filename", filename))
			}
		}()
	}

	//slog.Info("上传文件结果", slog.Any("err", err), slog.String("filename", filename), slog.Any("attachment", attachment))

	return attachment, err
}

func (halo *Halo) AddPost(title, slug, content, typ, status string, order int32, created int64, allowComment bool, tags, categories []string, cover string) (*consolesdk.Post, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			extension.GFM,
			extension.DefinitionList,
			extension.Footnote,
			extension.Typographer,
			extension.CJK,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(content), &buf); err != nil {
		return nil, err
	}

	publish := true
	if typ == "post_draft" {
		publish = false
	}

	var visible string
	switch status {
	case "publish":
		visible = visiblePublic
	case "hidden":
		visible = visiblePrivate
	case "private":
		visible = visiblePrivate
	case "waiting":
		visible = visiblePublic
		publish = false
	default:
		visible = visiblePrivate
	}

	// 处理头图
	var coverPtr *string
	if cover != "" {
		coverPtr = &cover
	}

	post, _, err := halo.console.PostV1alpha1ConsoleAPI.DraftPost(context.Background()).PostRequest(consolesdk.PostRequest{
		Content: &consolesdk.ContentUpdateParam{
			Content: consolesdk.PtrString(buf.String()),
			Raw:     consolesdk.PtrString(content),
			RawType: consolesdk.PtrString(contentRawType),
			Version: consolesdk.PtrInt64(contentVersion),
		},
		Post: consolesdk.Post{
			ApiVersion: apiVersion,
			Kind:       kindPost,
			Metadata: consolesdk.Metadata{
				Name: uuid.New().String(),
			},
			Spec: consolesdk.PostSpec{
				Title:        title,
				Slug:         slug,
				Deleted:      false,
				Publish:      publish,
				PublishTime:  consolesdk.PtrTime(time.Unix(created, 0)),
				Pinned:       false,
				AllowComment: allowComment,
				Visible:      visible,
				Priority:     order,
				Excerpt: consolesdk.Excerpt{
					AutoGenerate: true,
				},
				Categories: categories,
				Tags:       tags,
				HtmlMetas:  []map[string]string{},
				Cover:      coverPtr,
			},
		},
	}).Execute()
	if err != nil {
		return nil, err
	}

	return post, nil
}

func (halo *Halo) AddPage(title, slug, content, typ, status string, order int32, created int64, allowComment bool) (*consolesdk.SinglePage, error) {

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			extension.GFM,
			extension.DefinitionList,
			extension.Footnote,
			extension.Typographer,
			extension.CJK,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(content), &buf); err != nil {
		return nil, err
	}

	publish := true
	if typ == "post_draft" {
		publish = false
	}

	var visible string
	switch status {
	case "publish":
		visible = visiblePublic
	case "hidden":
		visible = visiblePrivate
	case "private":
		visible = visiblePrivate
	case "waiting":
		visible = visiblePublic
		publish = false
	default:
		visible = visiblePrivate
	}

	page, _, err := halo.console.SinglePageV1alpha1ConsoleAPI.DraftSinglePage(context.Background()).SinglePageRequest(consolesdk.SinglePageRequest{
		Content: consolesdk.ContentUpdateParam{
			Content: consolesdk.PtrString(buf.String()),
			Raw:     consolesdk.PtrString(content),
			RawType: consolesdk.PtrString(contentRawType),
			Version: consolesdk.PtrInt64(contentVersion),
		},
		Page: consolesdk.SinglePage{
			ApiVersion: apiVersion,
			Kind:       kindSinglePage,
			Metadata: consolesdk.Metadata{
				Name: uuid.New().String(),
			},
			Spec: consolesdk.SinglePageSpec{
				AllowComment: allowComment,
				Deleted:      false,
				Excerpt: consolesdk.Excerpt{
					AutoGenerate: true,
				},
				HtmlMetas:   []map[string]string{},
				Pinned:      false,
				Priority:    order,
				Publish:     publish,
				PublishTime: consolesdk.PtrTime(time.Unix(created, 0)),
				Slug:        slug,
				Title:       title,
				Visible:     visible,
			},
		},
	}).Execute()

	return page, err
}

func (halo *Halo) AddComment(postName, ownerName, ownerEmail, ownerWebsite, content, status, ip, userAgent string, isPost bool, created int64) (*consolesdk.Comment, error) {
	slog.Debug("创建评论", slog.String("postName", postName), slog.String("ownerName", ownerName), 
		slog.String("ip", ip), slog.String("userAgent", userAgent), slog.Int64("created", created))

	if status == "spam" {
		slog.Debug("跳过垃圾评论")
		return nil, nil
	}

	// 处理评论审核状态
	// Typecho 状态: "approved" (通过), "waiting" (待审核), "spam" (垃圾)
	// Halo 评论默认通过 console API 创建，由 Halo 的审核系统处理
	// 这里记录状态信息以便调试
	if status == "waiting" {
		slog.Debug("评论待审核", slog.String("postName", postName), slog.String("ownerName", ownerName))
	} else if status == "approved" {
		slog.Debug("评论已通过审核", slog.String("postName", postName), slog.String("ownerName", ownerName))
	} else {
		slog.Debug("未知评论状态", slog.String("status", status), slog.String("postName", postName))
	}

	kind := kindPost
	if !isPost {
		kind = kindSinglePage
	}

	comment, _, err := halo.console.CommentV1alpha1ConsoleAPI.CreateComment(context.Background()).CommentRequest(consolesdk.CommentRequest{
		AllowNotification: consolesdk.PtrBool(true),
		Content:           content,
		Raw:               content,
		Owner: &consolesdk.CommentEmailOwner{
			DisplayName: consolesdk.PtrString(ownerName),
			Email:       consolesdk.PtrString(ownerEmail),
			Website:     consolesdk.PtrString(ownerWebsite),
		},
		SubjectRef: consolesdk.Ref{
			Group:   consolesdk.PtrString(commentGroup),
			Kind:    consolesdk.PtrString(kind),
			Name:    postName,
			Version: consolesdk.PtrString(commentVersion),
		},
	}).Execute()

	if err != nil {
		return nil, err
	}

	// 更新评论的额外信息（IP、UA、创建时间）
	if comment != nil {
		extComment, _, getErr := halo.extension.CommentV1alpha1API.GetComment(context.Background(), comment.Metadata.Name).Execute()
		if getErr == nil && extComment != nil {
			updated := false
			
			// 设置IP地址
			// 即使为空字符串也设置，确保前端能正确显示
			extComment.Spec.IpAddress = &ip
			updated = true
			
			// 设置UserAgent
			// 即使为空字符串也设置，确保前端能正确显示（而不是显示为空）
			extComment.Spec.UserAgent = &userAgent
			updated = true
			
			// 设置创建时间
			if created > 0 {
				// 使用UTC时间，避免时区问题
				creationTime := time.Unix(created, 0).UTC()
				extComment.Spec.CreationTime = &creationTime
				updated = true
				slog.Debug("设置评论创建时间", slog.Int64("timestamp", created), slog.String("time", creationTime.Format(time.RFC3339)))
			} else {
				// 如果创建时间为0或无效，使用当前时间作为默认值
				// 避免前端显示"2分钟前"等错误时间
				creationTime := time.Now().UTC()
				extComment.Spec.CreationTime = &creationTime
				updated = true
				slog.Warn("评论创建时间为0或无效，使用当前时间作为默认值", slog.Int64("created", created), slog.String("defaultTime", creationTime.Format(time.RFC3339)))
			}
			
			// 如果有更新，则提交
			if updated {
				// 第一次尝试更新
				_, _, updateErr := halo.extension.CommentV1alpha1API.UpdateComment(context.Background(), extComment.Metadata.Name).Comment(*extComment).Execute()
				if updateErr != nil {
					// 409 Conflict 可能表示资源版本冲突，需要重试
					if strings.Contains(updateErr.Error(), "409") {
						slog.Debug("更新评论元数据冲突，尝试重新获取并更新", slog.String("commentName", comment.Metadata.Name))
						// 重新获取最新的扩展评论
						latestExtComment, _, getErr := halo.extension.CommentV1alpha1API.GetComment(context.Background(), extComment.Metadata.Name).Execute()
						if getErr == nil && latestExtComment != nil {
							// 应用我们的字段更新到最新资源
							latestExtComment.Spec.IpAddress = &ip
							latestExtComment.Spec.UserAgent = &userAgent
							if created > 0 {
								creationTime := time.Unix(created, 0).UTC()
								latestExtComment.Spec.CreationTime = &creationTime
							} else {
								creationTime := time.Now().UTC()
								latestExtComment.Spec.CreationTime = &creationTime
							}
							// 第二次尝试更新
							_, _, retryErr := halo.extension.CommentV1alpha1API.UpdateComment(context.Background(), latestExtComment.Metadata.Name).Comment(*latestExtComment).Execute()
							if retryErr != nil {
								slog.Error("重试更新评论元数据失败", slog.String("commentName", comment.Metadata.Name), slog.Any("err", retryErr))
							} else {
								slog.Debug("成功更新评论元数据（重试后）", slog.String("commentName", comment.Metadata.Name), 
									slog.String("ip", ip), slog.String("userAgent", userAgent), slog.Int64("created", created))
							}
						} else {
							slog.Error("重新获取评论扩展信息失败", slog.String("commentName", comment.Metadata.Name), slog.Any("err", getErr))
						}
					} else {
						slog.Error("更新评论元数据失败", slog.String("commentName", comment.Metadata.Name), slog.Any("err", updateErr))
					}
				} else {
					slog.Debug("成功更新评论元数据", slog.String("commentName", comment.Metadata.Name), 
						slog.String("ip", ip), slog.String("userAgent", userAgent), slog.Int64("created", created))
				}
			}
		} else if getErr != nil {
			slog.Error("获取评论扩展信息失败", slog.String("commentName", comment.Metadata.Name), slog.Any("err", getErr))
		}
	}

	return comment, err
}

func (halo *Halo) AddReply(commentName, ownerName, ownerEmail, ownerWebsite, content, quote, ip, userAgent string, created int64) (*consolesdk.Reply, error) {
	slog.Debug("创建回复", slog.String("commentName", commentName), slog.String("ownerName", ownerName), 
		slog.String("ip", ip), slog.String("userAgent", userAgent), slog.Int64("created", created))

	reply, _, err := halo.console.CommentV1alpha1ConsoleAPI.CreateReply(context.Background(), commentName).ReplyRequest(consolesdk.ReplyRequest{
		AllowNotification: consolesdk.PtrBool(true),
		Owner: &consolesdk.CommentEmailOwner{
			DisplayName: consolesdk.PtrString(ownerName),
			Email:       consolesdk.PtrString(ownerEmail),
			Website:     consolesdk.PtrString(ownerWebsite),
		},
		QuoteReply: consolesdk.PtrString(quote),
		Content:    content,
		Raw:        content,
	}).Execute()

	if err != nil {
		return nil, err
	}

	// 更新回复的额外信息（IP、UA、创建时间）
	if reply != nil {
		extReply, _, getErr := halo.extension.ReplyV1alpha1API.GetReply(context.Background(), reply.Metadata.Name).Execute()
		if getErr == nil && extReply != nil {
			updated := false
			
			// 设置IP地址
			// 即使为空字符串也设置，确保前端能正确显示
			extReply.Spec.IpAddress = &ip
			updated = true
			
			// 设置UserAgent
			// 即使为空字符串也设置，确保前端能正确显示（而不是显示为空）
			extReply.Spec.UserAgent = &userAgent
			updated = true
			
			// 设置创建时间
			if created > 0 {
				// 使用UTC时间，避免时区问题
				creationTime := time.Unix(created, 0).UTC()
				extReply.Spec.CreationTime = &creationTime
				updated = true
				slog.Debug("设置回复创建时间", slog.Int64("timestamp", created), slog.String("time", creationTime.Format(time.RFC3339)))
			} else {
				// 如果创建时间为0或无效，使用当前时间作为默认值
				// 避免前端显示"2分钟前"等错误时间
				creationTime := time.Now().UTC()
				extReply.Spec.CreationTime = &creationTime
				updated = true
				slog.Warn("回复创建时间为0或无效，使用当前时间作为默认值", slog.Int64("created", created), slog.String("defaultTime", creationTime.Format(time.RFC3339)))
			}
			
			// 如果有更新，则提交
			if updated {
				// 第一次尝试更新
				_, _, updateErr := halo.extension.ReplyV1alpha1API.UpdateReply(context.Background(), extReply.Metadata.Name).Reply(*extReply).Execute()
				if updateErr != nil {
					// 409 Conflict 可能表示资源版本冲突，需要重试
					if strings.Contains(updateErr.Error(), "409") {
						slog.Debug("更新回复元数据冲突，尝试重新获取并更新", slog.String("replyName", reply.Metadata.Name))
						// 重新获取最新的扩展回复
						latestExtReply, _, getErr := halo.extension.ReplyV1alpha1API.GetReply(context.Background(), extReply.Metadata.Name).Execute()
						if getErr == nil && latestExtReply != nil {
							// 应用我们的字段更新到最新资源
							latestExtReply.Spec.IpAddress = &ip
							latestExtReply.Spec.UserAgent = &userAgent
							if created > 0 {
								creationTime := time.Unix(created, 0).UTC()
								latestExtReply.Spec.CreationTime = &creationTime
							} else {
								creationTime := time.Now().UTC()
								latestExtReply.Spec.CreationTime = &creationTime
							}
							// 第二次尝试更新
							_, _, retryErr := halo.extension.ReplyV1alpha1API.UpdateReply(context.Background(), latestExtReply.Metadata.Name).Reply(*latestExtReply).Execute()
							if retryErr != nil {
								slog.Error("重试更新回复元数据失败", slog.String("replyName", reply.Metadata.Name), slog.Any("err", retryErr))
							} else {
								slog.Debug("成功更新回复元数据（重试后）", slog.String("replyName", reply.Metadata.Name), 
									slog.String("ip", ip), slog.String("userAgent", userAgent), slog.Int64("created", created))
							}
						} else {
							slog.Error("重新获取回复扩展信息失败", slog.String("replyName", reply.Metadata.Name), slog.Any("err", getErr))
						}
					} else {
						slog.Error("更新回复元数据失败", slog.String("replyName", reply.Metadata.Name), slog.Any("err", updateErr))
					}
				} else {
					slog.Debug("成功更新回复元数据", slog.String("replyName", reply.Metadata.Name), 
						slog.String("ip", ip), slog.String("userAgent", userAgent), slog.Int64("created", created))
				}
			}
		} else if getErr != nil {
			slog.Error("获取回复扩展信息失败", slog.String("replyName", reply.Metadata.Name), slog.Any("err", getErr))
		}
	}

	return reply, err
}
