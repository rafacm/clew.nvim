--- clew.nvim -- Ariadne's thread for your codebase.
---
--- Go-to-definition and find-references from a precomputed SCIP index, with no
--- language server resident in your editor.

local config = require("clew.config")

local M = {}

---@type boolean
local did_setup = false

--- @param opts ClewConfig|nil
function M.setup(opts)
  if did_setup then return end
  did_setup = true

  local cfg = config.setup(opts)

  require("clew.commands").setup()

  local ok, err = require("clew.lsp").register(cfg)
  if not ok then
    -- Not fatal: the commands still work, and :checkhealth clew explains why.
    vim.notify(
      ("clew: %s\nRun :checkhealth clew for details."):format(err),
      vim.log.levels.WARN,
      { title = "clew" }
    )
  end

  if cfg.auto_index == "save" then
    local group = vim.api.nvim_create_augroup("ClewAutoIndex", { clear = true })
    vim.api.nvim_create_autocmd("BufWritePost", {
      group = group,
      pattern = vim.tbl_map(function(ft) return "*." .. ft end, { "java", "kt", "ts", "tsx", "js" }),
      callback = function() require("clew.commands").index() end,
      desc = "clew: rebuild index after write",
    })
  end
end

--- Resolve the project root for a path (or the current buffer).
---@param path string|nil
---@return string|nil
function M.root(path)
  path = path or vim.api.nvim_buf_get_name(0)
  return (require("clew.root").find(path, config.options.root.markers))
end

M.index = function(unit) require("clew.commands").index(unit) end
M.status = function() require("clew.commands").status() end
M.restart = function() require("clew.commands").restart() end

return M
