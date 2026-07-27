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
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}
	if len(repo.categories) != 1 {
		t.Fatalf("category was not created")
	}
}

func TestCreateCategory_InvalidType(t *testing.T) {
	repo := newFakeCategoryRepo()
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	// type may only be income|expense -> the "oneof" binding fails
	w := performRequest(r, "POST", "/categories", `{"name":"Maas","type":"salary"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// A user must see both global (user_id NULL) and their own categories,
// but never another user's.
func TestListCategories_ScopedToUser(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Global Gider", Type: "expense", UserID: nil})
	repo.seed(&models.Category{ID: 2, Name: "Benim Maas", Type: "income", UserID: intPtr(1)})
	repo.seed(&models.Category{ID: 3, Name: "Baskasinin", Type: "income", UserID: intPtr(2)})

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/categories", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got []models.Category
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response could not be parsed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 categories (global + own), got %d: %s", len(got), w.Body.String())
	}
	for _, cat := range got {
		if cat.Name == "Baskasinin" {
			t.Fatalf("another user's category leaked")
		}
	}
}

func TestUpdateCategory_OwnCategory(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Eski", Type: "income", UserID: intPtr(1)})
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "PUT", "/categories/1", `{"name":"Yeni","type":"expense"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.categories[1].Name != "Yeni" || repo.categories[1].Type != "expense" {
		t.Fatalf("category was not updated: %+v", repo.categories[1])
	}
}

// Only an admin may modify a global category (user_id NULL).
func TestUpdateCategory_GlobalRequiresAdmin(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Global", Type: "expense", UserID: nil})

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "PUT", "/categories/1", `{"name":"Denedim","type":"income"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a client, got %d", w.Code)
	}
	if repo.categories[1].Name != "Global" {
		t.Fatalf("a client modified a global category")
	}
}

func TestUpdateCategory_AdminCanUpdateGlobal(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Global", Type: "expense", UserID: nil})

	r := setupCategoryRouter(repo, 99, models.RoleAdmin)
	w := performRequest(r, "PUT", "/categories/1", `{"name":"Guncellendi","type":"expense"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d", w.Code)
	}
}

// Another user's category must not be touched.
func TestUpdateCategory_OtherUsersCategoryForbidden(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Baskasinin", Type: "income", UserID: intPtr(2)})

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "PUT", "/categories/1", `{"name":"Hack","type":"income"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDeleteCategory_Success(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Silinecek", Type: "income", UserID: intPtr(1)})
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/categories/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(repo.categories) != 0 {
		t.Fatalf("category was not deleted")
	}
}

// A category referenced by a transaction cannot be deleted -> 409 Conflict.
func TestDeleteCategory_InUseReturnsConflict(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Kullanimda", Type: "income", UserID: intPtr(1)})
	repo.inUse[1] = true

	r := setupCategoryRouter(repo, 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/categories/1", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestDeleteCategory_NotFound(t *testing.T) {
	repo := newFakeCategoryRepo()
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/categories/999", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// SECURITY/BUDGET: a category referenced by a budget cannot be deleted.
func TestDeleteCategory_UsedByBudgetReturnsConflict(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 3, Name: "Market", Type: "expense", UserID: intPtr(1)})
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})

	r := setupCategoryRouterWithBudgets(repo, bRepo, 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/categories/3", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (body: %s)", w.Code, w.Body.String())
	}
	if _, ok := repo.categories[3]; !ok {
		t.Fatalf("a category referenced by a budget was deleted")
	}
}

// Regression: ordinary deletion must still work after the budget dependency
// check was introduced.
func TestDeleteCategory_NotUsedByBudgetSucceeds(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 3, Name: "Market", Type: "expense", UserID: intPtr(1)})
	bRepo := newFakeBudgetRepo()

	r := setupCategoryRouterWithBudgets(repo, bRepo, 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/categories/3", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if _, ok := repo.categories[3]; ok {
		t.Fatalf("category was not deleted")
	}
}
