package handlers

import (
	"GoGinMoneyCopilot/models"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupCategoryRouter(repo *fakeCategoryRepo, userID int, role models.Role) *gin.Engine {
	return setupCategoryRouterWithBudgets(repo, newFakeBudgetRepo(), userID, role)
}

func setupCategoryRouterWithBudgets(repo *fakeCategoryRepo, bRepo *fakeBudgetRepo, userID int, role models.Role) *gin.Engine {
	h := NewCategoryHandler(repo, bRepo)
	r := gin.New()
	r.Use(authAs(userID, role))
	r.POST("/categories", h.CreateCategory)
	r.GET("/categories", h.ListCategories)
	r.PUT("/categories/:id", h.UpdateCategory)
	r.DELETE("/categories/:id", h.DeleteCategory)
	return r
}

func intPtr(v int) *int { return &v }

func TestCreateCategory_Success(t *testing.T) {
	repo := newFakeCategoryRepo()
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "POST", "/categories", `{"name":"Maas","type":"income"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("beklenen 201, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if len(repo.categories) != 1 {
		t.Fatalf("kategori oluşmadı")
	}
}

func TestCreateCategory_InvalidType(t *testing.T) {
	repo := newFakeCategoryRepo()
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	// type sadece income|expense olabilir -> binding "oneof" başarısız
	w := performRequest(r, "POST", "/categories", `{"name":"Maas","type":"salary"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
	}
}

// Kullanıcı hem global (user_id NULL) hem kendi kategorilerini görmeli,
// başkasınınkini görmemeli.
func TestListCategories_ScopedToUser(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Global Gider", Type: "expense", UserID: nil})
	repo.seed(&models.Category{ID: 2, Name: "Benim Maas", Type: "income", UserID: intPtr(1)})
	repo.seed(&models.Category{ID: 3, Name: "Baskasinin", Type: "income", UserID: intPtr(2)})

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/categories", "")

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}

	var got []models.Category
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("cevap parse edilemedi: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("2 kategori bekleniyordu (global + kendi), gelen %d: %s", len(got), w.Body.String())
	}
	for _, cat := range got {
		if cat.Name == "Baskasinin" {
			t.Fatalf("başka kullanıcının kategorisi sızdı")
		}
	}
}

func TestUpdateCategory_OwnCategory(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Eski", Type: "income", UserID: intPtr(1)})
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "PUT", "/categories/1", `{"name":"Yeni","type":"expense"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	if repo.categories[1].Name != "Yeni" || repo.categories[1].Type != "expense" {
		t.Fatalf("kategori güncellenmedi: %+v", repo.categories[1])
	}
}

// Global kategoriyi (user_id NULL) sadece admin değiştirebilir.
func TestUpdateCategory_GlobalRequiresAdmin(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Global", Type: "expense", UserID: nil})

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "PUT", "/categories/1", `{"name":"Denedim","type":"income"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("client için 403 bekleniyordu, gelen %d", w.Code)
	}
	if repo.categories[1].Name != "Global" {
		t.Fatalf("client global kategoriyi değiştirdi")
	}
}

func TestUpdateCategory_AdminCanUpdateGlobal(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Global", Type: "expense", UserID: nil})

	r := setupCategoryRouter(repo, 99, models.RoleAdmin)
	w := performRequest(r, "PUT", "/categories/1", `{"name":"Guncellendi","type":"expense"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("admin için 200 bekleniyordu, gelen %d", w.Code)
	}
}

// Başka kullanıcının kategorisine dokunulamaz.
func TestUpdateCategory_OtherUsersCategoryForbidden(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Baskasinin", Type: "income", UserID: intPtr(2)})

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "PUT", "/categories/1", `{"name":"Hack","type":"income"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("beklenen 403, gelen %d", w.Code)
	}
}

func TestDeleteCategory_Success(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Silinecek", Type: "income", UserID: intPtr(1)})
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/categories/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	if len(repo.categories) != 0 {
		t.Fatalf("kategori silinmedi")
	}
}

// Bir transaction tarafından kullanılan kategori silinemez -> 409 Conflict.
func TestDeleteCategory_InUseReturnsConflict(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Kullanimda", Type: "income", UserID: intPtr(1)})
	repo.inUse[1] = true

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/categories/1", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("beklenen 409, gelen %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestDeleteCategory_NotFound(t *testing.T) {
	repo := newFakeCategoryRepo()
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/categories/999", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("beklenen 404, gelen %d", w.Code)
	}
}

// GÜVENLİK/BÜTÇE: bir bütçe tarafından kullanılan kategori silinemez.
func TestDeleteCategory_UsedByBudgetReturnsConflict(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 3, Name: "Market", Type: "expense", UserID: intPtr(1)})
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})

	r := setupCategoryRouterWithBudgets(repo, bRepo, 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/categories/3", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("beklenen 409, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if _, ok := repo.categories[3]; !ok {
		t.Fatalf("bütçede kullanılan kategori silindi")
	}
}

// Regresyon: bütçe bağımlılığı eklendikten sonra normal silme hâlâ çalışmalı.
func TestDeleteCategory_NotUsedByBudgetSucceeds(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 3, Name: "Market", Type: "expense", UserID: intPtr(1)})
	bRepo := newFakeBudgetRepo()

	r := setupCategoryRouterWithBudgets(repo, bRepo, 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/categories/3", "")

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if _, ok := repo.categories[3]; ok {
		t.Fatalf("kategori silinmedi")
	}
}
