from typing import Optional, List
import requests
import logging
from backend.settings.settings import settings

logger = logging.getLogger(__name__)

class MultiModelAdapter:
    """
    多模型适配器
    根据模型提供商类型获取可用模型列表
    """

    @classmethod
    def get_available_models(cls, provider: str, api_key: str = None, base_url: str = None) -> List[str]:
        """
        获取指定提供商的可用模型列表

        Args:
            provider: 模型提供商
            api_key: API密钥
            base_url: API基础URL

        Returns:
            模型ID列表
        """
        try:
            if provider == "ollama":
                return cls._get_ollama_models(base_url)
            elif provider == "gemini":
                return cls._get_gemini_models(api_key)
            # lm studio虽然格式不完全一致，但是也能用_get_openai_compatible_models
            else:
                return cls._get_openai_compatible_models(provider, api_key, base_url)
        except Exception as e:
            logger.error(f"获取 {provider} 模型列表失败: {e}")
            # 重新抛出异常，让调用方处理错误信息
            raise e

    @classmethod
    def _get_ollama_models(cls, base_url: str = None):
        """获取Ollama可用模型列表"""
        try:
            response = requests.get(f"{base_url}/api/tags", timeout=10)
            if response.status_code == 200:
                data = response.json()
                models = []
                for model_data in data.get("models", []):
                    model_name = model_data.get("name", "")
                    # 保留完整的模型名称，包括标签（如 qwen3:0.6b）
                    # 不再截断冒号后的部分

                    models.append(model_name)
                return models
            else:
                logger.warning(f"Ollama服务不可用: {response.status_code}")
                return []
        except requests.exceptions.RequestException as e:
            logger.warning(f"无法连接到Ollama服务: {e}")
            return []

    @classmethod
    def _get_gemini_models(cls, api_key: str = None) -> List[str]:
        """获取Gemini可用模型列表"""
        try:
            if not api_key:
                raise Exception("API密钥不能为空")

            # 使用Gemini原生API端点
            url = f"https://generativelanguage.googleapis.com/v1/models?key={api_key}"
            response = requests.get(url, timeout=10)

            if response.status_code == 200:
                data = response.json()
                models = []
                for model_data in data.get("models", []):
                    model_name = model_data.get("name", "")
                    # 去掉"models/"前缀
                    if model_name.startswith("models/"):
                        model_name = model_name[7:]  # 去掉"models/"（7个字符）
                    models.append(model_name)
                    print(f"获得的Gemini模型: {model_name}")
                return models
            else:
                error_detail = f"api key连接失败，请确定apikey可用 (HTTP {response.status_code})"
                try:
                    error_data = response.json()
                    if 'error' in error_data:
                        error_detail = f"{error_data['error'].get('message', error_data['error'])} (HTTP {response.status_code})"
                except:
                    pass
                raise Exception(error_detail)
        except requests.exceptions.RequestException as e:
            raise Exception(f"api key连接失败，请确定apikey可用 ({str(e)})")

    @classmethod
    def _get_openai_compatible_models(cls, provider: str, api_key: str = None, base_url: str = None) -> List[str]:
        """获取OpenAI兼容提供商的模型列表"""
        try:
            headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
            response = requests.get(f"{base_url}/models", headers=headers, timeout=10)

            if response.status_code == 200:
                data = response.json()
                models = []
                for model_data in data.get("data", []):
                    model_id = model_data.get("id", "")
                    models.append(model_id)
                    print(f"获得的id是{model_id}")

                # 添加嵌入模型列表
                if provider == "dashscope":
                    embedding_models = [
                        "text-embedding-v4",
                        "text-embedding-v3",
                        "text-embedding-v2",
                        "text-embedding-v1"
                    ]
                    models.extend(embedding_models)
                elif provider == "zhipuai":
                    embedding_models = [
                        "embedding-3",
                        "embedding-2"
                    ]
                    models.extend(embedding_models)
                elif provider == "openrouter":
                    embedding_models = [
                        "qwen/qwen3-embedding-8b",
                        "qwen/qwen3-embedding-4b"
                    ]
                    models.extend(embedding_models)

                return models
            else:
                # 如果API调用失败，抛出错误，包含原始状态码和响应内容
                error_detail = f"api key连接失败，请确定apikey可用 (HTTP {response.status_code})"
                try:
                    # 尝试获取更详细的错误信息
                    error_data = response.json()
                    if 'error' in error_data:
                        error_detail = f"{error_data['error']} (HTTP {response.status_code})"
                    elif 'message' in error_data:
                        error_detail = f"{error_data['message']} (HTTP {response.status_code})"
                except:
                    # 如果无法解析JSON，使用状态码
                    pass
                raise Exception(error_detail)
        except requests.exceptions.RequestException as e:
            raise Exception(f"api key连接失败，请确定apikey可用 ({str(e)})")

# 创建全局适配器实例
multi_model_adapter = MultiModelAdapter()
