import os
import time
import logging
from pathlib import Path

from backend.settings.settings import settings

logger = logging.getLogger(__name__)


def _ensure_data_subdirs(data_dir: Path):
    """检查并创建 data 下所有一级文件夹"""
    expected_dirs = [
        "config",
        "chromadb",
        "db",
        "uploads",
        "temp",
        "skills",
    ]
    for name in expected_dirs:
        subdir = data_dir / name
        if not subdir.exists():
            os.makedirs(subdir, exist_ok=True)
            logger.info(f"创建数据目录: {subdir}")


def _initialize_git(data_dir: Path):
    """通过Go启动器服务初始化Git仓库"""
    try:
        import httpx
    except ImportError:
        logger.warning("httpx未安装，跳过Git仓库初始化")
        return

    git_dir = data_dir / ".git"
    if git_dir.exists():
        logger.info("Git仓库已存在，跳过初始化")
        return

    git_service_url = os.environ.get("GIT_SERVICE_URL", "http://localhost:18080")
    url = f"{git_service_url}/api/checkpoints/init"

    logger.info(f"正在通过Go服务初始化Git仓库: {url}")

    try:
        resp = httpx.post(url, timeout=30.0)
        resp.raise_for_status()
        result = resp.json()
        if result.get("success"):
            logger.info(f"Git仓库初始化成功: {result.get('message', '')}")
        else:
            logger.warning(f"Git仓库初始化返回: {result.get('message', '')}")
    except httpx.ConnectError as e:
        logger.error(f"无法连接到Git服务 {url}: {e}")
    except httpx.HTTPStatusError as e:
        logger.error(f"Git服务返回错误 {e.response.status_code}: {e.response.text}")
    except Exception as e:
        logger.error(f"Git仓库初始化失败: {e}")


def initialize_directories_and_files():
    """
    初始化data目录下的所有目录和文件
    1. 确保 data 下所有一级目录存在
    2. 确保 .env 文件存在
    3. 通过Go服务初始化 Git 仓库
    """
    data_dir = Path(settings.DATA_DIR)
    chromadb_dir = Path(settings.CHROMADB_PERSIST_DIR)
    db_dir = Path(settings.DB_DIR)
    uploads_dir = Path(settings.UPLOADS_DIR)
    temp_dir = Path(settings.TEMP_DIR)
    skills_dir = Path(settings.SKILLS_DIR)
    env_file = settings.ENV_FILE_PATH
    
    # 1. 确保 data 下所有一级目录存在
    _ensure_data_subdirs(data_dir)
    
    # 2. 确保其他必要的目录存在（env 文件的父目录等）
    directories = [chromadb_dir, db_dir, uploads_dir, temp_dir, skills_dir, env_file.parent]
    for directory in directories:
        os.makedirs(directory, exist_ok=True)
    
    # 3. 确保 .env 文件存在，不存在则创建空文件
    if not env_file.exists():
        env_file.write_text("", encoding='utf-8')
        logger.info(f"创建 .env 文件: {env_file}")
    
    # 4. 通过Go服务初始化Git仓库
    _initialize_git(data_dir)
