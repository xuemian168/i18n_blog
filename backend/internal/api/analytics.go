package api

import (
	"net/http"
	"time"
	"blog-backend/internal/database"
	"blog-backend/internal/models"
	"github.com/gin-gonic/gin"
)

type AnalyticsResponse struct {
	TotalViews      int64                    `json:"total_views"`
	TotalArticles   int64                    `json:"total_articles"`
	ViewsToday      int64                    `json:"views_today"`
	ViewsThisWeek   int64                    `json:"views_this_week"`
	ViewsThisMonth  int64                    `json:"views_this_month"`
	TopArticles     []ArticleViewStats       `json:"top_articles"`
	RecentViews     []DailyViewStats         `json:"recent_views"`
	CategoryStats   []CategoryViewStats      `json:"category_stats"`
}

type ArticleViewStats struct {
	ID          uint   `json:"id"`
	Title       string `json:"title"`
	ViewCount   uint   `json:"view_count"`
	Category    string `json:"category"`
	CreatedAt   string `json:"created_at"`
}

type DailyViewStats struct {
	Date   string `json:"date"`
	Views  int64  `json:"views"`
}

type CategoryViewStats struct {
	Category   string `json:"category"`
	ViewCount  int64  `json:"view_count"`
	ArticleCount int64 `json:"article_count"`
}

func GetAnalytics(c *gin.Context) {
	lang := c.Query("lang")
	if lang == "" {
		lang = "zh" // Default language
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekAgo := today.AddDate(0, 0, -7)
	monthAgo := today.AddDate(0, -1, 0)

	// Get total views and articles
	var totalViews int64
	database.DB.Model(&models.Article{}).Select("COALESCE(SUM(view_count), 0)").Scan(&totalViews)
	
	var totalArticles int64
	database.DB.Model(&models.Article{}).Count(&totalArticles)

	// Get views for different time periods
	var viewsToday, viewsThisWeek, viewsThisMonth int64
	
	// Views today
	database.DB.Model(&models.ArticleView{}).
		Where("created_at >= ?", today).
		Count(&viewsToday)
	
	// Views this week
	database.DB.Model(&models.ArticleView{}).
		Where("created_at >= ?", weekAgo).
		Count(&viewsThisWeek)
	
	// Views this month
	database.DB.Model(&models.ArticleView{}).
		Where("created_at >= ?", monthAgo).
		Count(&viewsThisMonth)

	// Get top articles by view count with language support
	var topArticles []ArticleViewStats
	if lang == "zh" {
		// Default language - use original data
		database.DB.Model(&models.Article{}).
			Select("articles.id, articles.title, articles.view_count, categories.name as category, articles.created_at").
			Joins("LEFT JOIN categories ON articles.category_id = categories.id").
			Where("articles.deleted_at IS NULL").
			Order("articles.view_count DESC").
			Limit(10).
			Scan(&topArticles)
	} else {
		// Non-default language - use translations
		database.DB.Raw(`
			SELECT 
				a.id,
				COALESCE(at.title, a.title) as title,
				a.view_count,
				COALESCE(ct.name, c.name) as category,
				a.created_at
			FROM articles a
			LEFT JOIN categories c ON a.category_id = c.id
			LEFT JOIN article_translations at ON a.id = at.article_id AND at.language = ?
			LEFT JOIN category_translations ct ON c.id = ct.category_id AND ct.language = ?
			WHERE a.deleted_at IS NULL
			ORDER BY a.view_count DESC
			LIMIT 10
		`, lang, lang).Scan(&topArticles)
	}

	// Get daily view stats for the last 30 days
	var recentViews []DailyViewStats
	thirtyDaysAgo := today.AddDate(0, 0, -30)
	
	rows, err := database.DB.Raw(`
		SELECT DATE(created_at) as date, COUNT(*) as views 
		FROM article_views 
		WHERE created_at >= ? 
		GROUP BY DATE(created_at) 
		ORDER BY date DESC
	`, thirtyDaysAgo).Rows()
	
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var stat DailyViewStats
			if err := rows.Scan(&stat.Date, &stat.Views); err == nil {
				recentViews = append(recentViews, stat)
			}
		}
	}

	// Get category statistics with language support
	var categoryStats []CategoryViewStats
	if lang == "zh" {
		// Default language - use original data
		database.DB.Raw(`
			SELECT 
				c.name as category,
				COALESCE(SUM(a.view_count), 0) as view_count,
				COUNT(a.id) as article_count
			FROM categories c
			LEFT JOIN articles a ON c.id = a.category_id AND a.deleted_at IS NULL
			WHERE c.deleted_at IS NULL
			GROUP BY c.id, c.name
			ORDER BY view_count DESC
		`).Scan(&categoryStats)
	} else {
		// Non-default language - use translations
		database.DB.Raw(`
			SELECT 
				COALESCE(ct.name, c.name) as category,
				COALESCE(SUM(a.view_count), 0) as view_count,
				COUNT(a.id) as article_count
			FROM categories c
			LEFT JOIN articles a ON c.id = a.category_id AND a.deleted_at IS NULL
			LEFT JOIN category_translations ct ON c.id = ct.category_id AND ct.language = ?
			WHERE c.deleted_at IS NULL
			GROUP BY c.id, c.name, ct.name
			ORDER BY view_count DESC
		`, lang).Scan(&categoryStats)
	}

	response := AnalyticsResponse{
		TotalViews:     totalViews,
		TotalArticles:  totalArticles,
		ViewsToday:     viewsToday,
		ViewsThisWeek:  viewsThisWeek,
		ViewsThisMonth: viewsThisMonth,
		TopArticles:    topArticles,
		RecentViews:    recentViews,
		CategoryStats:  categoryStats,
	}

	c.JSON(http.StatusOK, response)
}

func GetArticleAnalytics(c *gin.Context) {
	articleID := c.Param("id")
	lang := c.Query("lang")
	if lang == "" {
		lang = "zh" // Default language
	}
	
	// Get article basic info with language support
	var article models.Article
	if err := database.DB.Preload("Category").Preload("Translations").First(&article, articleID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Article not found"})
		return
	}
	
	// Apply translation if needed
	if lang != "zh" && lang != "" {
		applyTranslation(&article, lang)
	}

	// Get unique visitors count
	var uniqueVisitors int64
	database.DB.Model(&models.ArticleView{}).
		Where("article_id = ?", articleID).
		Count(&uniqueVisitors)

	// Get daily views for this article in the last 30 days
	var dailyViews []DailyViewStats
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	
	rows, err := database.DB.Raw(`
		SELECT DATE(created_at) as date, COUNT(*) as views 
		FROM article_views 
		WHERE article_id = ? AND created_at >= ?
		GROUP BY DATE(created_at) 
		ORDER BY date DESC
	`, articleID, thirtyDaysAgo).Rows()
	
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var stat DailyViewStats
			if err := rows.Scan(&stat.Date, &stat.Views); err == nil {
				dailyViews = append(dailyViews, stat)
			}
		}
	}

	// Get recent visitors (last 10)
	var recentVisitors []struct {
		IPAddress   string    `json:"ip_address"`
		UserAgent   string    `json:"user_agent"`
		CreatedAt   time.Time `json:"created_at"`
	}
	
	database.DB.Model(&models.ArticleView{}).
		Select("ip_address, user_agent, created_at").
		Where("article_id = ?", articleID).
		Order("created_at DESC").
		Limit(10).
		Scan(&recentVisitors)

	response := gin.H{
		"article":          article,
		"unique_visitors":  uniqueVisitors,
		"total_views":      article.ViewCount,
		"daily_views":      dailyViews,
		"recent_visitors":  recentVisitors,
	}

	c.JSON(http.StatusOK, response)
}