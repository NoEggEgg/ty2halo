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
	"strings"
	"sync"
)

type App struct {
	conf *config.Config

	typecho     *service.Typecho
	halo        *service.Halo
	fileManager *service.FileManager

	typechoAttachments []*model.TypechoContents

	haloTagMap      sync.Map
	haloCategoryMap sync.Map
	haloFileMap     sync.Map
	haloPostMap     sync.Map
	haloPageMap     sync.Map
	haloCommentMap  sync.Map
	haloReplyMap    sync.Map
}

func NewApp(conf *config.Config) (*App, error) {
	a := &App{
		conf: conf,
	}

	if t, e := service.NewTypecho(conf.Typecho); e != nil {
		return nil, e
	} else {
		a.typecho = t
	}

	slog.Info("配置信息", slog.Any("conf", conf))

	if f, e := service.NewFileManager(conf.FileManager); e != nil {
		return nil, e
	} else {
		slog.Info("文件管理器", slog.Any("conf", conf.FileManager))
		a.fileManager = f
	}

	a.halo = service.NewHalo(conf.Halo)

	return a, nil
}

func (app *App) Run() error {
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

func (app *App) migrateTags() error {
	typechoTags, err := app.typecho.GetTags()
	if err != nil {
		return err
	}

	var tag *extensionsdk.Tag
	for _, typechoTag := range typechoTags {
		if tag, err = app.halo.AddTag(cast.ToString(typechoTag.Name), cast.ToString(typechoTag.Slug)); err != nil {
			return err
		}
		app.haloTagMap.Store(typechoTag.Mid, tag)
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
		if category, err = app.halo.AddCategory(cast.ToString(typechoCategory.Name), cast.ToString(typechoCategory.Slug), cast.ToString(typechoCategory.Description), cast.ToInt32(typechoCategory.Order_)); err != nil {
			return err
		}
		app.haloCategoryMap.Store(typechoCategory.Mid, category)
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
	}

	return nil
}

func (app *App) migrateComments() error {
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

		var haloComment *consolesdk.Comment
		if haloComment, err = app.halo.AddComment(
			contentName,
			cast.ToString(typechoComment.Author),
			cast.ToString(typechoComment.Mail),
			cast.ToString(typechoComment.URL),
			cast.ToString(typechoComment.Text),
			cast.ToString(typechoComment.Status),
			isPost,
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

// extractFirstImage 从Markdown内容中提取第一张远程图片URL
// 支持格式: ![alt](url) 和 <img src="url">
// 只返回以 http:// 或 https:// 开头的远程图片URL
// 如果没有找到远程图片，返回空字符串
func extractFirstImage(content string) string {
	// 1. 匹配Markdown图片: ![...](http://...)
	mdPattern := `!\[.*?\]\((https?://[^\)]+)\)`
	if matches := findStringSubmatch(content, mdPattern); len(matches) > 1 {
		url := matches[1]
		if isImageUrl(url) {
			return url
		}
	}
	
	// 2. 匹配HTML img标签: <img src="http://...">
	htmlPattern := `<img[^>]+src=["'](https?://[^"']+)["']`
	if matches := findStringSubmatch(content, htmlPattern); len(matches) > 1 {
		url := matches[1]
		if isImageUrl(url) {
			return url
		}
	}
	
	return ""
}

// isImageUrl 检查URL是否是图片链接
func isImageUrl(url string) bool {
	lowerUrl := strings.ToLower(url)
	// 常见的图片后缀
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg", ".ico"}
	for _, ext := range imageExts {
		if strings.HasSuffix(lowerUrl, ext) {
			return true
		}
	}
	// 如果没有后缀，但包含图片相关关键词，也认为是图片
	if strings.Contains(lowerUrl, ".jpg") || 
	   strings.Contains(lowerUrl, ".png") || 
	   strings.Contains(lowerUrl, ".gif") ||
	   strings.Contains(lowerUrl, "image") {
		return true
	}
	// 排除已知的非图片文件
	nonImageExts := []string{".js", ".css", ".pdf", ".zip", ".tar", ".gz", ".doc", ".docx"}
	for _, ext := range nonImageExts {
		if strings.HasSuffix(lowerUrl, ext) {
			return false
		}
	}
	// 默认情况下，如果是远程URL且没有明确的后缀，也认为是图片（可能是动态图片链接）
	return true
}

// findStringSubmatch 简单的正则匹配辅助函数
func findStringSubmatch(content, pattern string) []string {
	// 使用strings进行简单匹配，避免引入regexp包
	// 匹配 ![alt](url) 格式
	if strings.Contains(pattern, `!\[`) {
		startIdx := strings.Index(content, `![`)
		for startIdx != -1 {
			// 找到对应的 ](
			midIdx := strings.Index(content[startIdx:], `](`)
			if midIdx == -1 {
				break
			}
			midIdx += startIdx + 2
			
			// 找到对应的 )
			endIdx := strings.Index(content[midIdx:], `)`)
			if endIdx == -1 {
				break
			}
			endIdx += midIdx
			
			url := content[midIdx:endIdx]
			// 检查是否是远程URL
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				// 排除常见的非图片后缀
				lowerUrl := strings.ToLower(url)
				if !strings.HasSuffix(lowerUrl, ".js") && 
				   !strings.HasSuffix(lowerUrl, ".css") &&
				   !strings.HasSuffix(lowerUrl, ".pdf") {
					return []string{"", url}
				}
			}
			
			// 继续查找下一个
			content = content[endIdx:]
			startIdx = strings.Index(content, `![`)
		}
	}
	
	// 匹配 <img src="url"> 格式
	if strings.Contains(pattern, `<img`) {
		lowerContent := strings.ToLower(content)
		startIdx := strings.Index(lowerContent, `<img`)
		for startIdx != -1 {
			// 找到 src=" 或 src='
			srcIdx := strings.Index(lowerContent[startIdx:], `src=`)
			if srcIdx == -1 {
				break
			}
			srcIdx += startIdx + 4
			
			// 确定引号类型
			quoteChar := lowerContent[srcIdx]
			if quoteChar != '"' && quoteChar != '\'' {
				// 跳过这个img标签
				lowerContent = lowerContent[srcIdx:]
				startIdx = strings.Index(lowerContent, `<img`)
				continue
			}
			
			urlStart := srcIdx + 1
			urlEnd := strings.Index(lowerContent[urlStart:], string(quoteChar))
			if urlEnd == -1 {
				break
			}
			urlEnd += urlStart
			
			url := content[urlStart:urlEnd]
			// 检查是否是远程URL
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				lowerUrl := strings.ToLower(url)
				if !strings.HasSuffix(lowerUrl, ".js") && 
				   !strings.HasSuffix(lowerUrl, ".css") &&
				   !strings.HasSuffix(lowerUrl, ".pdf") {
					return []string{"", url}
				}
			}
			
			// 继续查找下一个
			lowerContent = lowerContent[urlEnd:]
			startIdx = strings.Index(lowerContent, `<img`)
		}
	}
	
	return nil
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
