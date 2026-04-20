package migrate

import (
	consolesdk "api.halo.run/apis/openapi-go-console"
	extensionsdk "api.halo.run/apis/openapi-go-extension"
	"fmt"
	"github.com/fghwett/typecho-to-halo/config"
	"github.com/fghwett/typecho-to-halo/pkg/typecho/model"
	"github.com/fghwett/typecho-to-halo/service"
	"github.com/spf13/cast"
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

type App struct {
	conf *config.Config

	typecho     *service.Typecho
	halo        *service.Halo
	fileManager *service.FileManager

	typechoAttachments []*model.TypechoContents

	haloTagMap      sync.Map // key: mid(uint32) 或 slug(string) -> *extensionsdk.Tag
	haloCategoryMap sync.Map // key: mid(uint32) 或 slug(string) -> *extensionsdk.Category
	haloFileMap     sync.Map
	haloPostMap     sync.Map // key: cid(uint32) 或 slug(string) -> *consolesdk.Post
	haloPageMap     sync.Map // key: cid(uint32) 或 slug(string) -> *consolesdk.SinglePage
	haloCommentMap  sync.Map // key: coid -> *consolesdk.Comment
	haloReplyMap    sync.Map // key: coid -> *consolesdk.Reply

	// 去重 map：slug -> true
	haloTagSlugMap      sync.Map
	haloCategorySlugMap sync.Map
	haloPostSlugMap     sync.Map
	haloPageSlugMap     sync.Map
}

func NewApp(conf *config.Config) (*App, error) {
	a := &App{
		conf: conf,
	}

	t, e := service.NewTypecho(conf.Typecho)
	if e != nil {
		return nil, e
	}
	a.typecho = t

	slog.Info("配置信息", slog.Any("conf", conf))

	f, e := service.NewFileManager(conf.FileManager)
	if e != nil {
		return nil, e
	}
	slog.Info("文件管理器", slog.Any("conf", conf.FileManager))
	a.fileManager = f

	a.halo = service.NewHalo(conf.Halo)

	return a, nil
}

func (app *App) Run() error {
	// 预加载已有数据，用于去重
	if err := app.loadExistingData(); err != nil {
		slog.Warn("加载已有数据失败，跳过去重检查", slog.Any("err", err))
	}

	actions := []*Action{
		NewAction("迁移标签", app.migrateTags),
		NewAction("迁移分类", app.migrateCategories),
		// NewAction("迁移附件", app.migrateAttachments),
		NewAction("迁移文章", app.migratePosts),
		NewAction("迁移页面", app.migratePages),
		NewAction("迁移评论", app.migrateComments),
	}

	return app.doActions(actions)
}

func (app *App) loadExistingData() error {
	slog.Info("开始加载 Halo 已有数据...")

	// 加载已有标签（按 slug 索引，用于去重）
	tags, err := app.halo.GetTags()
	if err != nil {
		return fmt.Errorf("加载标签失败: %w", err)
	}
	for _, tag := range tags {
		app.haloTagMap.Store(tag.Spec.Slug, &tag)
		app.haloTagSlugMap.Store(tag.Spec.Slug, true)
	}
	slog.Info("已加载标签", slog.Int("count", len(tags)))

	// 加载已有分类（按 slug 索引，用于去重）
	categories, err := app.halo.GetCategories()
	if err != nil {
		return fmt.Errorf("加载分类失败: %w", err)
	}
	for _, category := range categories {
		app.haloCategoryMap.Store(category.Spec.Slug, &category)
		app.haloCategorySlugMap.Store(category.Spec.Slug, true)
	}
	slog.Info("已加载分类", slog.Int("count", len(categories)))

	// 加载已有文章
	posts, err := app.halo.GetPosts()
	if err != nil {
		return fmt.Errorf("加载文章失败: %w", err)
	}
	for _, post := range posts {
		app.haloPostMap.Store(post.Post.Spec.Slug, &post.Post)
		app.haloPostSlugMap.Store(post.Post.Spec.Slug, true)
	}
	slog.Info("已加载文章", slog.Int("count", len(posts)))

	// 加载已有页面
	pages, err := app.halo.GetPages()
	if err != nil {
		return fmt.Errorf("加载页面失败: %w", err)
	}
	for _, page := range pages {
		app.haloPageMap.Store(page.Page.Spec.Slug, &page.Page)
		app.haloPageSlugMap.Store(page.Page.Spec.Slug, true)
	}
	slog.Info("已加载页面", slog.Int("count", len(pages)))

	// 建立 Typecho mid → Halo Tag/Category 映射（通过 slug 桥接）
	// 这样 migratePosts 用 relationShip.Mid 查找时能找到对应的 Halo 对象
	typechoTags, _ := app.typecho.GetTags()
	for _, typechoTag := range typechoTags {
		slug := cast.ToString(typechoTag.Slug)
		if value, ok := app.haloTagMap.Load(slug); ok {
			app.haloTagMap.Store(typechoTag.Mid, value) // mid -> Halo Tag（兼容原代码查询方式）
		}
	}

	typechoCategories, _ := app.typecho.GetCategories()
	for _, typechoCategory := range typechoCategories {
		slug := cast.ToString(typechoCategory.Slug)
		if value, ok := app.haloCategoryMap.Load(slug); ok {
			app.haloCategoryMap.Store(typechoCategory.Mid, value) // mid -> Halo Category（兼容原代码查询方式）
		}
	}

	// 建立 Typecho cid → Halo Post/Page 映射（通过 slug 桥接）
	// 评论迁移用 contentId（cid）查找 Post/Page 的 Metadata.Name
	typechoPosts, _ := app.typecho.GetPosts()
	for _, typechoPost := range typechoPosts {
		slug := cast.ToString(typechoPost.Slug)
		if value, ok := app.haloPostMap.Load(slug); ok {
			app.haloPostMap.Store(typechoPost.Cid, value) // cid -> Halo Post
		}
	}

	typechoPagesList, _ := app.typecho.GetPage()
	for _, typechoPage := range typechoPagesList {
		slug := cast.ToString(typechoPage.Slug)
		if value, ok := app.haloPageMap.Load(slug); ok {
			app.haloPageMap.Store(typechoPage.Cid, value) // cid -> Halo Page
		}
	}

	return nil
}

func (app *App) migrateTags() error {
	typechoTags, err := app.typecho.GetTags()
	if err != nil {
		return err
	}

	var tag *extensionsdk.Tag
	for _, typechoTag := range typechoTags {
		slug := cast.ToString(typechoTag.Slug)

		// 去重检查
		if _, ok := app.haloTagSlugMap.Load(slug); ok {
			slog.Info("标签已存在，跳过", slog.String("slug", slug), slog.String("name", cast.ToString(typechoTag.Name)))
			// 仍需存入 map 以便文章关联
			if existing, ok := app.haloTagMap.Load(slug); ok {
				// 用 mid 作为 key 重新存储
				app.haloTagMap.Store(typechoTag.Mid, existing)
			}
			continue
		}

		if tag, err = app.halo.AddTag(cast.ToString(typechoTag.Name), slug); err != nil {
			return err
		}
		app.haloTagMap.Store(typechoTag.Mid, tag)
		app.haloTagSlugMap.Store(slug, true)
	}

	return nil
}

func (app *App) migrateCategories() error {
	typechoCategories, err := app.typecho.GetCategories()
	if err != nil {
		return err
	}

	var category *extensionsdk.Category
	for _, typechoCategory := range typechoCategories {
		slug := cast.ToString(typechoCategory.Slug)

		// 去重检查
		if _, ok := app.haloCategorySlugMap.Load(slug); ok {
			slog.Info("分类已存在，跳过", slog.String("slug", slug), slog.String("name", cast.ToString(typechoCategory.Name)))
			// mid -> Halo Category 映射已由 loadExistingData 建立
			continue
		}

		if category, err = app.halo.AddCategory(cast.ToString(typechoCategory.Name), slug, cast.ToString(typechoCategory.Description), cast.ToInt32(typechoCategory.Order_)); err != nil {
			return err
		}
		app.haloCategoryMap.Store(typechoCategory.Mid, category)
		app.haloCategorySlugMap.Store(slug, true)
	}

	cache := make(map[uint32][]string)
	for _, typechoCategory := range typechoCategories {
		parentId := cast.ToUint32(typechoCategory.Parent)
		if parentId == 0 {
			continue
		}

		mid := typechoCategory.Mid
		value, ok := app.haloCategoryMap.Load(mid)
		if !ok {
			continue
		}
		childCategory := value.(*extensionsdk.Category)
		list, exist := cache[parentId]
		if exist {
			list = append(list, childCategory.Metadata.Name)
			cache[parentId] = list
			continue
		}
		cache[parentId] = []string{childCategory.Metadata.Name}
	}
	slog.Info("分类父级", slog.Any("cache", cache))

	for mid, children := range cache {
		value, ok := app.haloCategoryMap.Load(mid)
		if !ok {
			continue
		}
		parentCategory := value.(*extensionsdk.Category)
		parentCategory.Spec.Children = children

		if category, err = app.halo.AddCategoryChildren(parentCategory.Metadata.Name, children); err != nil {
			return err
		}
		app.haloCategoryMap.Store(mid, category)
	}

	return nil
}

func (app *App) migrateAttachments() error {
	typechoFiles, err := app.typecho.GetAttachments()
	if err != nil {
		return err
	}
	slog.Info("文件数量", slog.Any("count", len(typechoFiles)))

	app.typechoAttachments = typechoFiles

	var attachment *consolesdk.Attachment
	for _, typechoFile := range typechoFiles {

		link := app.typecho.GetAttachmentUrl(typechoFile)

		var filename *string
		if filename, err = app.fileManager.DownFile(link); err != nil {
			return err
		}
		slog.Info("下载文件", slog.String("filename", cast.ToString(filename)))

		if attachment, err = app.halo.AddAttachment(cast.ToString(filename)); err != nil {
			return err
		}

		app.haloFileMap.Store(typechoFile.Cid, attachment)

		// 删除文件
		//if err = app.fileManager.DeleteFile(cast.ToString(filename)); err != nil {
		//	return err
		//}

	}

	return nil
}

func (app *App) migratePosts() error {
	typechoPosts, err := app.typecho.GetPosts()
	if err != nil {
		return err
	}

	var relationShips []*model.TypechoRelationships
	if relationShips, err = app.typecho.GetRelationShips(); err != nil {
		return err
	}

	var post *consolesdk.Post
	for _, typechoPost := range typechoPosts {
		slug := cast.ToString(typechoPost.Slug)

		// 去重检查
		if _, ok := app.haloPostSlugMap.Load(slug); ok {
			slog.Info("文章已存在，跳过", slog.String("slug", slug), slog.String("title", cast.ToString(typechoPost.Title)))
			// cid -> Halo Post 映射：通过 slug 桥接（评论迁移需要 Post.Metadata.Name）
			if value, ok := app.haloPostMap.Load(slug); ok {
				app.haloPostMap.Store(typechoPost.Cid, value)
			}
			continue
		}
		var tags, categories []string
		for _, relationShip := range relationShips {
			if relationShip.Cid != typechoPost.Cid {
				continue
			}
			value, ok := app.haloCategoryMap.Load(relationShip.Mid)
			if ok {
				haloCategory := value.(*extensionsdk.Category)
				categories = append(categories, haloCategory.Metadata.Name)
				continue
			}
			value, ok = app.haloTagMap.Load(relationShip.Mid)
			if !ok {
				continue
			}
			haloTag := value.(*extensionsdk.Tag)
			tags = append(tags, haloTag.Metadata.Name)
		}

		// 处理文章内容
		content := cast.ToString(typechoPost.Text)
		
		// 提取第一张远程图片作为头图
		cover := extractFirstImage(content)
		
		for _, typechoAttachment := range app.typechoAttachments {
			value, ok := app.haloFileMap.Load(typechoAttachment.Cid)
			if !ok {
				continue
			}
			typechoFileUrl := app.typecho.GetAttachmentUrl(typechoAttachment)

			haloAttachment := value.(*consolesdk.Attachment)
			//haloFileUrl := fmt.Sprintf("%s://%s/upload/%s", app.conf.Halo.Schema, app.conf.Halo.Host, haloAttachment.Spec.GetDisplayName())
			haloFileUrl := fmt.Sprintf("/upload/%s", haloAttachment.Spec.GetDisplayName())

			content = strings.ReplaceAll(content, typechoFileUrl, haloFileUrl)
		}
		content = strings.ReplaceAll(content, `<!--markdown-->`, ``)

		if post, err = app.halo.AddPost(
			cast.ToString(typechoPost.Title),
			cast.ToString(typechoPost.Slug),
			content,
			cast.ToString(typechoPost.Type),
			cast.ToString(typechoPost.Status),
			cast.ToInt32(typechoPost.Order_),
			cast.ToInt64(typechoPost.Created),
			cast.ToBool(typechoPost.AllowComment),
			tags,
			categories,
			cover,
		); err != nil {
			return err
		}

		app.haloPostMap.Store(typechoPost.Cid, post)
		app.haloPostSlugMap.Store(slug, true)
	}

	return nil
}

func (app *App) migratePages() error {
	typechoPages, err := app.typecho.GetPage()
	if err != nil {
		return err
	}

	var haloPage *consolesdk.SinglePage
	for _, typechoPage := range typechoPages {
		slug := cast.ToString(typechoPage.Slug)

		// 去重检查
		if _, ok := app.haloPageSlugMap.Load(slug); ok {
			slog.Info("页面已存在，跳过", slog.String("slug", slug), slog.String("title", cast.ToString(typechoPage.Title)))
			// cid -> Halo Page 映射：通过 slug 桥接（评论迁移需要 Page.Metadata.Name）
			if value, ok := app.haloPageMap.Load(slug); ok {
				app.haloPageMap.Store(typechoPage.Cid, value)
			}
			continue
		}

		content := cast.ToString(typechoPage.Text)
		for _, typechoAttachment := range app.typechoAttachments {
			value, ok := app.haloFileMap.Load(typechoAttachment.Cid)
			if !ok {
				continue
			}
			typechoFileUrl := app.typecho.GetAttachmentUrl(typechoAttachment)

			haloAttachment := value.(*consolesdk.Attachment)
			//haloFileUrl := fmt.Sprintf("%s://%s/upload/%s", app.conf.Halo.Schema, app.conf.Halo.Host, haloAttachment.Spec.GetDisplayName())
			haloFileUrl := fmt.Sprintf("/upload/%s", haloAttachment.Spec.GetDisplayName())

			content = strings.ReplaceAll(content, typechoFileUrl, haloFileUrl)
		}

		if haloPage, err = app.halo.AddPage(
			cast.ToString(typechoPage.Title),
			cast.ToString(typechoPage.Slug),
			content,
			cast.ToString(typechoPage.Type),
			cast.ToString(typechoPage.Status),
			cast.ToInt32(typechoPage.Order_),
			cast.ToInt64(typechoPage.Created),
			cast.ToBool(typechoPage.AllowComment),
		); err != nil {
			return err
		}

		app.haloPageMap.Store(typechoPage.Cid, haloPage)
		app.haloPageSlugMap.Store(slug, true)
	}

	return nil
}

func (app *App) migrateComments() error {
	// 自动继续评论迁移（跳过用户确认）
	slog.Info("开始迁移评论...")

	allTypechoComments, err := app.typecho.GetComments()
	if err != nil {
		return err
	}

	var (
		typechoComments       []*model.TypechoComments
		typechoReplyMap       = make(map[uint32][]*model.TypechoComments)
		typechoReplyParentMap = make(map[uint32]uint32)
	)

	for _, typechoComment := range allTypechoComments {
		parentCommentId := cast.ToUint32(typechoComment.Parent)
		typechoReplyParentMap[typechoComment.Coid] = parentCommentId
		if parentCommentId == 0 {
			typechoComments = append(typechoComments, typechoComment)
			continue
		}
		list, ok := typechoReplyMap[parentCommentId]
		if !ok {
			typechoReplyMap[parentCommentId] = []*model.TypechoComments{
				typechoComment,
			}
			continue
		}
		list = append(list, typechoComment)
		typechoReplyMap[parentCommentId] = list
	}

	var nextIds []uint32
	for _, typechoComment := range typechoComments {
		contentId := cast.ToUint32(typechoComment.Cid)

		var contentName string
		var isPost bool
		if value, ok := app.haloPostMap.Load(contentId); ok {
			haloPost := value.(*consolesdk.Post)
			contentName = haloPost.Metadata.Name
			isPost = true
		} else if value, ok = app.haloPageMap.Load(contentId); ok {
			haloPage := value.(*consolesdk.SinglePage)
			contentName = haloPage.Metadata.Name
		}
		if contentName == "" {
			continue
		}

		// 记录字段状态，帮助诊断信息缺失问题
		ip := cast.ToString(typechoComment.IP)
		agent := cast.ToString(typechoComment.Agent)
		created := cast.ToInt64(typechoComment.Created)
		
		if typechoComment.IP == nil {
			slog.Warn("评论IP字段为nil，将使用空字符串", slog.Uint64("coid", uint64(typechoComment.Coid)))
		}
		if typechoComment.Agent == nil {
			slog.Warn("评论Agent字段为nil，将使用空字符串", slog.Uint64("coid", uint64(typechoComment.Coid)))
		}
		if typechoComment.Created == nil || created <= 0 {
			slog.Warn("评论Created字段无效，将使用当前时间作为默认值", slog.Uint64("coid", uint64(typechoComment.Coid)), slog.Int64("created", created))
		}

		var haloComment *consolesdk.Comment
		if haloComment, err = app.halo.AddComment(
			contentName,
			cast.ToString(typechoComment.Author),
			cast.ToString(typechoComment.Mail),
			cast.ToString(typechoComment.URL),
			cast.ToString(typechoComment.Text),
			cast.ToString(typechoComment.Status),
			ip,
			agent,
			isPost,
			created,
		); err != nil {
			return err
		}

		if haloComment == nil {
			continue
		}

		nextIds = append(nextIds, typechoComment.Coid)
		app.haloCommentMap.Store(typechoComment.Coid, haloComment)
	}

	for {
		var newNextIds []uint32

		for _, typechoCommentId := range nextIds {
			typechoReplyList, ok := typechoReplyMap[typechoCommentId]
			if !ok {
				continue
			}
			for _, typechoReply := range typechoReplyList {
				// 记录字段状态，帮助诊断信息缺失问题
				replyIp := cast.ToString(typechoReply.IP)
				replyAgent := cast.ToString(typechoReply.Agent)
				replyCreated := cast.ToInt64(typechoReply.Created)
				
				if typechoReply.IP == nil {
					slog.Warn("回复IP字段为nil，将使用空字符串", slog.Uint64("coid", uint64(typechoReply.Coid)))
				}
				if typechoReply.Agent == nil {
					slog.Warn("回复Agent字段为nil，将使用空字符串", slog.Uint64("coid", uint64(typechoReply.Coid)))
				}
				if typechoReply.Created == nil || replyCreated <= 0 {
					slog.Warn("回复Created字段无效，将使用当前时间作为默认值", slog.Uint64("coid", uint64(typechoReply.Coid)), slog.Int64("created", replyCreated))
				}
				
				parentCommentId := cast.ToUint32(typechoReply.Parent)
				var commentName, quoteComment string
				if value, exist := app.haloCommentMap.Load(parentCommentId); exist {
					haloComment := value.(*consolesdk.Comment)
					commentName = haloComment.Metadata.Name
				} else if value, exist = app.haloReplyMap.Load(parentCommentId); exist {
					haloReply := value.(*consolesdk.Reply)
					quoteComment = haloReply.Metadata.Name
					// 找出第一条评论
					var parentId uint32
					childrenId := parentCommentId
					for {
						parentId, ok = typechoReplyParentMap[childrenId]
						if !ok {
							parentId = childrenId
							break
						}
						if parentId == 0 {
							parentId = childrenId
							break
						}
						childrenId = parentId
					}
					if value, exist = app.haloCommentMap.Load(parentId); exist {
						commentName = value.(*consolesdk.Comment).Metadata.Name
					}
				}
				if commentName == "" {
					continue
				}
				var haloReply *consolesdk.Reply
				if haloReply, err = app.halo.AddReply(
					commentName,
					cast.ToString(typechoReply.Author),
					cast.ToString(typechoReply.Mail),
					cast.ToString(typechoReply.URL),
					cast.ToString(typechoReply.Text),
					quoteComment,
					replyIp,      // 使用局部变量
					replyAgent,   // 使用局部变量
					replyCreated, // 使用局部变量
				); err != nil {
					return err
				}
				app.haloReplyMap.Store(typechoReply.Coid, haloReply)
				newNextIds = append(newNextIds, typechoReply.Coid)
			}
			delete(typechoReplyMap, typechoCommentId)

		}

		if len(typechoReplyMap) == 0 || len(newNextIds) == 0 {
			break
		}
		nextIds = newNextIds
	}

	return nil
}

type Action struct {
	Name string
	Do   func() error
}

func NewAction(name string, do func() error) *Action {
	return &Action{
		Name: name,
		Do:   do,
	}
}

var (
	// 匹配 ![alt](url) 和 ![alt](url "title")
	mdImageRe = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^\s\)]+?)(?:\s+"[^"]*")?\)`)
	// 匹配 <img src="url"> 和 <img src='url'>
	htmlImgRe = regexp.MustCompile(`<img[^>]+src=["'](https?://[^"']+)["']`)
)

// extractFirstImage 从内容中提取第一张远程图片URL作为封面图
// 支持格式: ![alt](url)、![alt](url "title") 和 <img src="url">
// 只返回以 http:// 或 https:// 开头的远程图片URL，未找到返回空字符串
func extractFirstImage(content string) string {
	// 1. 优先匹配 Markdown 图片
	if matches := mdImageRe.FindStringSubmatch(content); len(matches) > 1 {
		if isImageUrl(matches[1]) {
			return matches[1]
		}
	}
	// 2. 其次匹配 HTML img 标签
	if matches := htmlImgRe.FindStringSubmatch(content); len(matches) > 1 {
		if isImageUrl(matches[1]) {
			return matches[1]
		}
	}
	return ""
}

// isImageUrl 检查URL是否是图片链接
// 先剥离 ?query 和 CDN后缀（如 !water），再用 HasSuffix 判断路径后缀
func isImageUrl(url string) bool {
	lowerUrl := strings.ToLower(url)
	// 去除 ? 后面的查询参数
	pathPart := lowerUrl
	if qIdx := strings.Index(pathPart, "?"); qIdx != -1 {
		pathPart = pathPart[:qIdx]
	}
	// 去除 CDN 后缀参数，如 .webp!water
	if bangIdx := strings.Index(pathPart, "!"); bangIdx != -1 {
		pathPart = pathPart[:bangIdx]
	}
	// 用 HasSuffix 判断路径是否以图片后缀结尾
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg", ".ico", ".avif", ".heic"}
	for _, ext := range imageExts {
		if strings.HasSuffix(pathPart, ext) {
			return true
		}
	}
	return false
}

func (app *App) doActions(actions []*Action) error {
	for _, action := range actions {
		slog.Info("执行操作", slog.String("action", action.Name))
		if err := action.Do(); err != nil {
			slog.Error("执行操作失败", slog.String("action", action.Name), slog.Any("err", err))
			return err
		}
		slog.Info("执行操作成功", slog.String("action", action.Name))
	}
	return nil
}
